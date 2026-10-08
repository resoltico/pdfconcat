// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/layout"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type (
	// pipeline is the state of one build or check.
	pipeline struct {
		app      *App
		env      Env
		command  *cli.Command
		progress *progress
		builder  *report.Builder
		registry *capture.Registry
		// workspace owns every scratch file of the run; nil until the destination is checked.
		workspace *capture.Workspace
		captures  *capture.Set
		pdf       Engine
		// resource is the generated-pages document; nil when the job has no generated page.
		resource *pdfengine.ResourceDocument

		job    *assembly.Job
		flat   *assembly.Flattened
		fonts  loadedFonts
		files  []inspectedSource
		layout *assembly.Layout
		placed []*typeset.Placed

		// output and reportPath are absolute; empty when not named.
		outputDigest string
		output       string
		reportPath   string
		phases       report.Phases
		counts       report.Counts
		publication  report.Publication
		status       report.Status
		// warnings are cleanup problems that cannot enter the report.
		warnings []string

		// reportSettled is true once the outcome of the requested report is known, so a failure report
		// is not written over it.
		reportSettled bool
		// described is true once the parts of the job were added to the report.
		described bool
		// inventoryComplete is true once every plan, source, font, output and report file is known and
		// has passed the alias checks; failure reports may overwrite only after that.
		inventoryComplete bool
	}

	// loadedFonts are the fonts of the job, loaded once, by absolute file path; the empty path is the
	// built-in font.
	loadedFonts struct {
		byPath map[string]*typeset.Font
	}
)

// errStopped means the pipeline recorded its failure and should stop; the status is in pipeline.status.
var errStopped = errors.New("pipeline stopped")

// assemble runs a build or a check and prints its result.
func (a *App) assemble(ctx context.Context, command *cli.Command, env Env) int {
	current := &pipeline{
		app:      a,
		env:      env,
		command:  command,
		progress: newProgress(env.Progress, time.Now),
		builder:  report.NewBuilder(string(command.Name)),
		registry: capture.NewRegistry(),
		status:   report.StatusOK,
		phases: report.Phases{
			Instructions: report.PhaseIncomplete, InputInspection: report.PhaseNotRun,
			Layout: report.PhaseNotRun, OutputVerification: report.PhaseNotRun,
		},
		publication: report.Publication{ReportStatus: report.ReportNotRequested},
	}

	rep := current.run(ctx)

	current.progress.erase()

	for _, warning := range current.warnings {
		warn(env, warning)
	}

	return finishCommand(env, command, rep)
}

func (p *pipeline) isBuild() bool { return p.command.Name == cli.NameBuild }

// run executes the stages and returns the final report, ready to print.
func (p *pipeline) run(ctx context.Context) *report.Report {
	// Every stage that fails records its diagnostics and status before it returns errStopped; anything
	// else is a stage that broke that contract, and is recorded here rather than lost.
	err := p.execute(ctx)
	if err != nil && !errors.Is(err, errStopped) {
		found := classify(stageInput, err)
		p.builder.AddDiagnostic(0, found.diagnostic)
		p.status = found.status
	}

	return p.conclude(ctx)
}

// execute runs the stages; a stage that fails records its diagnostics and returns errStopped.
func (p *pipeline) execute(ctx context.Context) error {
	if diagnostic := workingDirectoryDiagnostic(p.command.Name, p.env.WorkingDir); diagnostic != nil {
		if p.command.ReportPath != "" {
			p.publication = report.Publication{
				ReportStatus:            report.ReportFailed,
				ReportWrite:             "not_written",
				ReportTargetObservation: "unknown",
				ReportFrom:              originalReportArgumentReference,
			}
			p.reportSettled = true
		}

		return p.stopWith(problem{diagnostic: *diagnostic, status: report.StatusFailed})
	}

	p.progress.enter(stagePrepare)

	steps := []func() error{
		p.resolveCommandPaths, func() error { return p.loadJob(ctx) }, p.resolveOutput, p.flatten, p.checkDestinations,
	}

	for _, step := range steps {
		err := step()
		if err != nil {
			return err
		}
	}

	return p.executeResources(ctx)
}

// executeResources runs the stages that need the private workspace.
func (p *pipeline) executeResources(ctx context.Context) error {
	err := p.openWorkspace()
	if err != nil {
		return err
	}

	defer p.closeWorkspace()

	inputSteps := []func() error{func() error { return p.loadFonts(ctx) }, func() error { return p.validateText(ctx) }, p.registerSources}
	for _, step := range inputSteps {
		err = step()
		if err != nil {
			return err
		}
	}

	p.phases.Instructions = report.PhaseComplete
	p.phases.InputInspection = report.PhaseIncomplete

	err = p.inspectSources(ctx)
	if err != nil {
		return err
	}

	p.phases.InputInspection = report.PhaseComplete
	p.phases.Layout = report.PhaseIncomplete

	err = p.resolveLayout(ctx)
	if err != nil {
		return err
	}

	if !p.isBuild() {
		return p.finishCheck(ctx)
	}

	return p.build(ctx)
}

// stop records one classified problem and ends the pipeline.
func (p *pipeline) stop(stage report.Stage, err error) error {
	return p.stopWith(classify(stage, err))
}

// stopWith records a problem and ends the pipeline.
func (p *pipeline) stopWith(found problem) error {
	p.builder.AddDiagnostic(0, found.diagnostic)
	p.status = found.status

	return errStopped
}

// stopAll records every diagnostic of a stage, keeping their order, and ends the pipeline.
func (p *pipeline) stopAll(status report.Status, diagnostics []report.Diagnostic) error {
	for index := range diagnostics {
		p.builder.AddDiagnostic(index, diagnostics[index])
	}

	p.status = status

	return errStopped
}

// stopAssembly records the located problems of a resolution step, in input order. Anything that is not a
// list of located problems is an interruption or an unexpected failure, classified at stage.
func (p *pipeline) stopAssembly(stage report.Stage, err error) error {
	list, ok := errors.AsType[assembly.Errors](err)
	if !ok {
		return p.stop(stage, err)
	}

	diagnostics := make([]report.Diagnostic, len(list))

	for index, item := range list {
		diagnostics[index] = report.Diagnostic{
			Stage: report.Stage(item.Stage), Code: report.Code(item.Code),
			Location: report.LocationOf(item.Location), Message: sharedMessage(item),
		}
		for _, consumer := range item.Consumers {
			diagnostics[index].Consumers = append(diagnostics[index].Consumers, p.flat.Source.Pointer(consumer.Ref, ""))
		}
	}

	return p.stopAll(report.StatusInvalid, diagnostics)
}

// cancelled records an interruption at stage if the context is done.
func (p *pipeline) cancelled(ctx context.Context, stage report.Stage) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}

	return p.stop(stage, err)
}

// abs makes path absolute against the working directory.
func (p *pipeline) abs(path string) string {
	return absolutePath(p.env.WorkingDir, path)
}

func absolutePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Join(base, path)
}

// jobCount is the number of inspection workers: --jobs, or min(4, GOMAXPROCS), never above the number of sources.
func (p *pipeline) jobCount(sources int) int {
	const defaultJobs = 4

	jobs := p.command.Jobs
	if jobs <= 0 {
		jobs = min(defaultJobs, runtime.GOMAXPROCS(0))
	}

	return max(min(jobs, sources), 1)
}

// openWorkspace creates the private scratch directory: beside the destination for a build, so staging and
// publication share a filesystem, and in the system temporary directory for a check.
func (p *pipeline) openWorkspace() error {
	var (
		workspace *capture.Workspace
		err       error
	)

	if p.isBuild() {
		workspace, err = capture.NewWorkspaceBeside(p.output)
	} else {
		workspace, err = capture.NewTemporaryWorkspace()
	}

	if err != nil {
		return p.stop(stageOutput, err)
	}

	p.workspace, p.captures = workspace, capture.NewSet(workspace)

	return nil
}

// closeWorkspace removes every scratch file. A cleanup failure cannot change what was already published,
// so it is a warning on standard error that names the directory to remove.
func (p *pipeline) closeWorkspace() {
	err := p.workspace.Close()
	if err != nil {
		p.warnings = append(p.warnings, fmt.Sprintf("could not remove the scratch directory %s: %v", p.workspace.Dir(), err))
	}
}

// resolveLayout resolves page geometry and then lays out the text of each distinct generated page.
func (p *pipeline) resolveLayout(ctx context.Context) error {
	geometry := make([]assembly.SourceGeometry, len(p.files))
	for index := range p.files {
		info := &p.files[index].info
		geometry[index] = assembly.SourceGeometry{
			First: assembly.PageGeometry{Width: info.First.Width, Height: info.First.Height},
			Last:  assembly.PageGeometry{Width: info.Last.Width, Height: info.Last.Height},
			Pages: int64(info.Pages),
		}
	}

	resolved, err := p.flat.Resolve(geometry, p.fonts.digest)
	if err != nil {
		return p.stopAssembly(stageLayout, err)
	}

	p.layout = resolved
	// Backend policy is source-known and must precede placement/rendering in check and build.
	if policyErr := p.assemblyPlan().Validate(); policyErr != nil {
		return p.stopWith(p.assembleProblem(policyErr))
	}

	p.counts = report.Counts{
		SourcePages: &resolved.Totals.Source, GeneratedPages: &resolved.Totals.Generated, TotalPages: &resolved.Totals.Total,
	}

	// A check renders nothing, so it keeps only the bounds and findings of each placement.
	place := layout.PlaceBounds

	if p.isBuild() {
		return p.placeResources(ctx)
	}

	p.placed, err = place(ctx, resolved, p.fonts.lookup)
	if err != nil {
		return p.stopAssembly(stageLayout, err)
	}

	p.phases.Layout = report.PhaseComplete

	return nil
}

// digest is the assembly.FontDigests of the loaded fonts.
func (f *loadedFonts) digest(path string) (assembly.FontDigest, bool) {
	font, found := f.byPath[path]
	if !found {
		return assembly.FontDigest{}, false
	}

	return assembly.FontDigest(font.Identity()), true
}

// lookup is the layout.FontSet of the loaded fonts.
func (f *loadedFonts) lookup(path string) (*typeset.Font, bool) {
	font, found := f.byPath[path]

	return font, found
}

// finishCheck describes the layout, publishes the requested report, and ends the check.
func (p *pipeline) finishCheck(ctx context.Context) error {
	if p.reportPath == "" {
		return nil
	}

	staged, err := p.stageReport(ctx)
	if err != nil {
		return err
	}

	p.beforePublish()

	err = staged.Publish(ctx, p.reportPolicy())
	p.reportCleanupWarning(staged.CleanupError())

	if err != nil {
		return p.reportFailure(err)
	}

	return nil
}

func (p *pipeline) validateText(ctx context.Context) error {
	if err := layout.ValidateText(ctx, p.flat, p.fonts.lookup); err != nil {
		return p.stopAssembly(stageLayout, err)
	}

	return nil
}
