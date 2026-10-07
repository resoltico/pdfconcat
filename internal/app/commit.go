// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/genpage"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/publish"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

const (
	codeReportTarget report.Code = "report_destination_invalid"
	// secondaryOrder sorts a secondary diagnostic after every primary one.
	secondaryOrder        = math.MaxInt
	scratchMode           = 0o600
	generatedPDFExtension = ".pdf"
)

// reportPolicy is how the requested report may replace an existing file. Once every input is known,
// --overwrite governs it exactly as it governs the PDF. Before that, the report may only be a new file:
// a malformed job might name the existing file as an input it never got to read.
func (p *pipeline) reportPolicy() publish.Policy {
	if !p.inventoryComplete {
		return publish.Policy{NoClobberOnly: true}
	}

	return publish.Policy{Overwrite: p.command.Overwrite}
}

// preflightReport fails early when the report destination cannot be used, before the costly stages.
func (p *pipeline) preflightReport() error {
	if p.reportPath == "" {
		return nil
	}

	err := publish.Preflight(p.reportPath, p.reportPolicy())
	if err != nil {
		return p.stopWith(problem{
			diagnostic: report.Diagnostic{Stage: stageOutput, Code: codeReportTarget, Path: p.reportPath, Message: err.Error()},
			status:     report.StatusInvalid,
		})
	}

	return nil
}

// snapshot builds the report as the run stands, with the current phases, counts and publication state.
func (p *pipeline) snapshot(status report.Status) *report.Report {
	p.builder.SetPhases(p.phases)
	p.builder.SetCounts(p.counts)
	p.publication.OutputDigest = p.outputDigest
	p.builder.SetPublication(p.publication)

	rep := p.builder.Build(status)
	rep.Producer = &report.Producer{
		Tool:     "pdfconcat",
		Version:  firstNonEmpty("devel", p.env.Build.Version),
		Commit:   firstNonEmpty("unknown", p.env.Build.Commit),
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}

	return rep
}

// stageFile writes rep beside the report target without making it visible.
func (p *pipeline) stageFile(ctx context.Context, target string, rep *report.Report) (*publish.Staged, error) {
	var buffer bytes.Buffer

	_, err := report.Write(&buffer, rep, report.MaxReportBytes)
	if err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}

	staged, err := publish.Stage(ctx, target, &buffer, report.MaxReportBytes)
	if err != nil {
		return nil, fmt.Errorf("stage report: %w", err)
	}

	staged.Verify = func() error { return p.verifyReport(target) }

	return staged, nil
}

// stageReport stages the complete report of a run that is about to succeed. It describes the state the
// run ends in once the report is published, so the saved file is true when it becomes visible, and, if
// only the report fails to publish, when its recovery copy is moved into place.
func (p *pipeline) stageReport(ctx context.Context) (*publish.Staged, error) {
	p.describe()

	p.publication = report.Publication{
		Output: p.output, Published: p.isBuild(), ReportStatus: report.ReportWritten, ReportPath: p.reportPath,
	}

	staged, err := p.stageFile(ctx, p.reportPath, p.snapshot(report.StatusOK))
	if err != nil {
		return nil, p.reportFailure(err)
	}

	return staged, nil
}

// reportFailure records that the requested report could not be produced before anything was published.
func (p *pipeline) reportFailure(err error) error {
	p.reportSettled = true
	p.publication = report.Publication{Output: p.output, ReportStatus: report.ReportFailed, ReportPath: p.reportPath}

	return p.stopWith(p.reportProblem(err))
}

// reportProblem classifies a failure to write the report.
func (p *pipeline) reportProblem(err error) problem {
	if reportErr, ok := report.AsError(err); ok {
		return problem{diagnostic: reportErr.Diagnostic, status: reportErr.Status()}
	}

	found := classify(stagePublish, err)
	if found.diagnostic.Code == codeOperationFailed || found.diagnostic.Code == codeSourceUnreadable {
		found.diagnostic.Code = codeReportWrite
	}

	if found.diagnostic.Path == "" {
		found.diagnostic.Path = p.reportPath
	}

	return found
}

// toInt converts a page count or page number that layout resolution already bounded by
// assembly.MaxOutputPages (the backend's 32-bit page numbers). The clamp only keeps the conversion
// total; the engine rejects a request whose runs do not add up to the expected pages.
func toInt(value int64) int {
	return int(min(max(value, 0), assembly.MaxOutputPages))
}

// build assembles the captured sources and already produced resource into a staged file, then publishes.
func (p *pipeline) build(ctx context.Context) error {
	staged := p.workspace.NewPath(generatedPDFExtension)

	err := p.merge(ctx, staged)
	if err != nil {
		return err
	}

	p.progress.enter(stagePersist)

	return p.publishAll(ctx, staged)
}

// order compiles the layout's runs into the engine's page order.
func (p *pipeline) order() []pdfengine.Run {
	order := make([]pdfengine.Run, len(p.layout.Runs))

	for index, run := range p.layout.Runs {
		if run.Kind == assembly.RunGenerated {
			order[index] = pdfengine.GeneratedPages(run.Index, toInt(run.Count))
		} else {
			order[index] = pdfengine.SourcePages(run.Index, toInt(run.FirstPage), toInt(run.Count))
		}
	}

	return order
}

// merge assembles the staged file and verifies it (the engine reads it back and compares its page count).
func (p *pipeline) merge(ctx context.Context, staged string) error {
	p.progress.enter(stageMerge)

	request := &pdfengine.AssembleRequest{
		Sources: make([]pdfengine.SourceFile, len(p.files)), Resource: p.resource, Order: p.order(),
		Destination: staged, ExpectedPages: toInt(p.layout.Totals.Total),
	}

	for index := range p.files {
		request.Sources[index] = pdfengine.SourceFile{Path: p.files[index].captured.Path, Info: p.files[index].info}
	}

	p.phases.OutputVerification = report.PhaseIncomplete

	err := p.pdf.Assemble(ctx, request)
	if err != nil {
		return p.stopWith(p.assembleProblem(err))
	}

	p.phases.OutputVerification = report.PhaseComplete
	p.outputDigest = request.OutputDigest

	return nil
}

// assembleProblem classifies an engine failure and names the source the engine blamed, by its own path
// rather than the private copy's.
func (p *pipeline) assembleProblem(err error) problem {
	found := classify(stageAssemble, err)

	engineErr, ok := errors.AsType[*pdfengine.Error](err)
	if ok && engineErr.Source >= 0 && engineErr.Source < len(p.flat.Files) {
		file := &p.flat.Files[engineErr.Source]
		found.diagnostic.Path = file.Path
		found.diagnostic.Location = p.at(file.FirstUse, "")
	}

	return found
}

// publishAll stages the report, rechecks both destinations, and publishes the PDF and then the report.
func (p *pipeline) publishAll(ctx context.Context, stagedPDF string) error {
	var stagedReport *publish.Staged

	if p.reportPath != "" {
		var err error

		stagedReport, err = p.stageReport(ctx)
		if err != nil {
			return err
		}
	}

	p.beforePublish()

	result, err := publish.Commit(ctx, publish.PDF{
		Staged: stagedPDF, Destination: p.output, Overwrite: p.command.Overwrite, AfterCommit: p.app.afterPDFCommit,
		Verify: func() error { _, err := p.registry.Add(capture.RoleOutput, p.output); return err },
	}, stagedReport, p.reportPolicy())

	return p.afterCommit(ctx, &result, err)
}

// afterCommit turns the outcome of publication into the state of the run. Once the PDF is visible nothing
// undoes it: the run reports what was committed, whatever happened to the report or to the process.
func (p *pipeline) afterCommit(ctx context.Context, result *publish.Result, err error) error {
	if !result.PDFPublished {
		// Commit discarded the staged report; saveFailureReport describes the failure in a new one.
		p.publication = report.Publication{Output: p.output, ReportStatus: report.ReportNotRequested}

		return p.stopWith(publishProblem(err))
	}

	p.reportCleanupWarning(result.ReportCleanupError)
	p.reportSettled = true
	p.publication = report.Publication{Output: p.output, Published: true, ReportStatus: report.ReportNotRequested}

	if p.reportPath != "" {
		p.publication.ReportPath, p.publication.ReportStatus = p.reportPath, report.ReportWritten
	}

	reportErr, failed := errors.AsType[*publish.ReportPublishError](err)
	if failed {
		return p.reportCommitFailure(context.WithoutCancel(ctx), result, reportErr)
	}

	durability, unflushed := errors.AsType[*publish.DurabilityError](err)
	if unflushed {
		p.warnings = append(p.warnings, durability.Error()+"; the output is visible but may not survive a crash")
	}

	return nil
}

func (p *pipeline) reportCleanupWarning(err error) {
	if err != nil {
		p.warnings = append(p.warnings, err.Error())
	}
}

// publishProblem classifies a failure that happened before the PDF became visible.
func publishProblem(err error) problem {
	found := classify(stagePublish, err)
	if found.diagnostic.Code == codeOperationFailed {
		found.diagnostic.Code = codePublishFailed
	}

	return found
}

// conclude produces the final report of the run. A failed run saves it to the requested report path,
// following the no-clobber rule until every input was known.
func (p *pipeline) conclude(ctx context.Context) *report.Report {
	p.describe()

	if p.publication.Output == "" {
		p.publication.Output = p.output
	}

	if p.status == report.StatusOK {
		return p.snapshot(report.StatusOK)
	}

	if p.reportPath == "" || p.reportSettled {
		return p.snapshot(p.status)
	}

	return p.saveFailureReport(ctx)
}

// saveFailureReport writes the failure report. If that fails, the failure is added as a secondary
// diagnostic and the primary status and diagnostics stay as they were.
func (p *pipeline) saveFailureReport(ctx context.Context) *report.Report {
	p.publication.ReportStatus, p.publication.ReportPath = report.ReportWritten, p.reportPath

	rep := p.snapshot(p.status)
	// An interrupted run still describes itself, so the write outlives the cancelled context.
	err := p.writeFailureReport(context.WithoutCancel(ctx), rep)
	if err == nil {
		return rep
	}

	found := p.reportProblem(err)
	found.diagnostic.Message = fmt.Sprintf("the failure report could not be saved to %s: %s", p.reportPath, found.diagnostic.Message)

	if !p.inventoryComplete {
		found.diagnostic.Message += "; until every input is known a report is only written to a new file, so --overwrite does not apply"
	}

	p.builder.AddDiagnostic(secondaryOrder, found.diagnostic)

	p.publication.ReportStatus = report.ReportFailed

	return p.snapshot(p.status)
}

// writeFailureReport registers the report path against every known file and publishes rep under the policy.
func (p *pipeline) writeFailureReport(ctx context.Context, rep *report.Report) error {
	_, err := p.registry.Add(capture.RoleReport, p.reportPath)
	if err != nil {
		return fmt.Errorf("register report: %w", err)
	}

	staged, err := p.stageFile(ctx, p.reportPath, rep)
	if err != nil {
		return err
	}

	err = staged.Publish(ctx, p.reportPolicy())
	p.reportCleanupWarning(staged.CleanupError())

	if err != nil {
		return fmt.Errorf("publish report: %w", err)
	}

	return nil
}

// recoveryMessage states first, within the preview a summary shows, that the PDF stands and what to do;
// the exact command and the cause follow.
func recoveryMessage(failure *publish.ReportPublishError) string {
	return fmt.Sprintf(
		"PDF published, report not saved. Do not rebuild: choose a distinct unused report target, copy recovery_report there "+
			"without overwriting (POSIX cp -n, Windows PowerShell File.Copy), then delete the recovery file. %s. Cause: %v",
		failure.Instruction, failure.Err)
}

// beforePublish runs the test hook that changes the world between the last check and publication.
func (p *pipeline) beforePublish() {
	if p.app.beforeCommit != nil {
		p.app.beforeCommit()
	}
}

// placeResources emits each distinct shaped page immediately, retaining only report geometry.
func (p *pipeline) placeResources(ctx context.Context) error {
	p.progress.enter(stageRender)

	count := len(p.layout.Specs)
	if count == 0 {
		p.phases.Layout = report.PhaseComplete
		return nil
	}

	path := p.workspace.NewPath(generatedPDFExtension)

	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, scratchMode)
	if err != nil {
		return p.stop(stageRender, err)
	}

	return p.produceResource(ctx, path, file)
}

// produceResource owns the open resource handle through writing, closing and outcome recording.
func (p *pipeline) produceResource(ctx context.Context, path string, file io.WriteCloser) error {
	count := len(p.layout.Specs)

	var (
		shaper   typeset.Shaper
		problems assembly.Errors
	)

	p.placed = make([]*typeset.Placed, count)
	writeErr := writeResource(ctx, file, count, p.resourceFonts(), func(index int) (genpage.Page, error) {
		placed, placementErr := layout.PlaceSpec(&shaper, p.layout, index, p.fonts.lookup)
		if placementErr != nil {
			problems = append(problems, assembly.Diagnostics(placementErr)...)
		}

		page := resourcePage(&p.layout.Specs[index].Spec)

		if placed != nil {
			metadata := *placed
			metadata.Lines = nil

			p.placed[index] = &metadata
			if placementErr == nil {
				page.Text = placed
			}
		}

		return page, nil
	})

	if len(problems) > 0 {
		return p.stopAssembly(stageLayout, problems)
	}

	if writeErr != nil {
		found := classify(stageRender, writeErr)
		if found.diagnostic.Code == codeOperationFailed {
			found.diagnostic.Code = codeRenderFailed
		}

		return p.stopWith(found)
	}

	p.resource = &pdfengine.ResourceDocument{Path: path, Pages: count}
	p.phases.Layout = report.PhaseComplete

	return nil
}

// resourceFonts lists the already loaded fonts needed by distinct generated pages.
func (p *pipeline) resourceFonts() []*typeset.Font {
	fonts := make([]*typeset.Font, 0, len(p.layout.Specs))
	for index := range p.layout.Specs {
		font, found := p.fonts.lookup(p.layout.Specs[index].Spec.Text.Font.File)
		if found {
			fonts = append(fonts, font)
		}
	}

	return fonts
}

// verifyReport protects every known input and the PDF at the report's commit boundary.
func (p *pipeline) verifyReport(target string) error {
	if p.output != "" {
		if _, err := p.registry.Add(capture.RoleOutput, p.output); err != nil {
			return err
		}
	}

	_, err := p.registry.Add(capture.RoleReport, target)

	return err
}

// writeResource preserves independent production and close failures.
func writeResource(ctx context.Context, file io.WriteCloser, count int, fonts []*typeset.Font, produce genpage.Producer) error {
	_, writeErr := genpage.WriteProduced(ctx, file, count, fonts, produce)

	closeErr := file.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("finish the generated-pages file: %w", closeErr)
	}

	return errors.Join(writeErr, closeErr)
}

func resourcePage(spec *assembly.BlankSpec) genpage.Page {
	page := genpage.Page{Width: float64(spec.Dim.Width), Height: float64(spec.Dim.Height), TextColor: genpage.Color(spec.Text.Color)}
	if spec.Background.Painted {
		color := genpage.Color(spec.Background.Color)
		page.Background = &color
	}

	return page
}
