// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// inspectedSource is one distinct source after capture and inspection.
type inspectedSource struct {
	captured capture.Captured
	info     pdfengine.SourceInfo
}

// maxFontBytes bounds a font file read into memory.
const maxFontBytes = 64 << 20

var (
	errNotDirectory = errors.New("not a directory")
	errFontTooLarge = errors.New("the font file is too large")
	errFontInvalid  = errors.New("the font file is not a usable TrueType font")
)

// at locates a value of the job for a diagnostic.
func (p *pipeline) at(origin assembly.Origin, member string) *report.Location {
	return report.LocationOf(assembly.Locate(p.flat.Source, origin, member))
}

// combine decides the status of several problems found by one stage: interruption wins, otherwise the
// first problem decides, so the status does not depend on timing.
func combine(problems []problem) report.Status {
	for _, found := range problems {
		if found.status == report.StatusInterrupted {
			return report.StatusInterrupted
		}
	}

	return problems[0].status
}

// loadFonts registers, captures and loads every font file of the job before any PDF is touched, so a
// bad font fails in milliseconds rather than after thousands of sources were read.
func (p *pipeline) loadFonts(ctx context.Context) error {
	p.fonts = loadedFonts{byPath: make(map[string]*typeset.Font)}

	if p.usesDefaultFont() {
		builtin, err := typeset.LoadDefaultFont()
		if err != nil {
			return p.stop(stageInput, err)
		}

		p.fonts.byPath[""] = builtin
	}

	var problems []problem

	for index := range p.flat.Fonts {
		use := &p.flat.Fonts[index]

		font, err := p.loadFont(ctx, use)
		if err != nil {
			for _, origin := range use.Declarations {
				problems = append(problems, p.fontFailure(use, origin, err))
			}

			continue
		}

		p.fonts.byPath[use.Path] = font
	}

	return p.stopProblems(problems)
}

// fontFailure builds the problem for a font that could not be used.
func (p *pipeline) fontFailure(use *assembly.FontFileUse, origin assembly.Origin, err error) problem {
	found := classify(stageInput, err)

	switch {
	case errors.Is(err, errFontTooLarge):
		found.diagnostic.Code, found.status = codeFontTooLarge, report.StatusInvalid
	case errors.Is(err, errFontInvalid):
		found.diagnostic.Code, found.status = codeFontInvalid, report.StatusInvalid
	case found.diagnostic.Code == codeSourceUnreadable:
		found.diagnostic.Code = codeFontUnreadable
	default:
	}

	found.diagnostic.Path = use.Path

	found.diagnostic.Location = p.at(origin, "/blank/text/font")
	for index := range p.flat.Contributions {
		contribution := &p.flat.Contributions[index]
		if contribution.Kind != assembly.ItemBlank {
			continue
		}

		if fontDeclarationOrigin(p.flat, contribution) != origin {
			continue
		}

		if p.flat.Styles[contribution.Style].Style.Text.Font.Value.File == use.Path {
			found.diagnostic.Consumers = append(found.diagnostic.Consumers, p.flat.Source.Pointer(contribution.Origin.Ref, ""))
		}
	}

	return found
}

// loadFont registers a font file, captures it once and validates it.
func (p *pipeline) loadFont(ctx context.Context, use *assembly.FontFileUse) (*typeset.Font, error) {
	_, err := p.registry.Add(capture.RoleFont, use.Path)
	if err != nil {
		return nil, fmt.Errorf("font file: %w", err)
	}

	captured, err := p.captures.Capture(ctx, use.Path)
	if err != nil {
		return nil, fmt.Errorf("font file: %w", err)
	}

	if captured.Size > maxFontBytes {
		return nil, fmt.Errorf("%w: %d bytes; the limit is %d", errFontTooLarge, captured.Size, int64(maxFontBytes))
	}

	data, err := os.ReadFile(captured.Path)
	if err != nil {
		return nil, &capture.ScratchError{Dir: p.workspace.Dir(), Operation: "read the copy of " + use.Path, Err: err}
	}

	font, err := typeset.LoadFont(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFontInvalid, err)
	}

	return font, nil
}

// stopProblems records the problems of one stage, in the order given, and ends the pipeline; it does
// nothing when there are none.
func (p *pipeline) stopProblems(problems []problem) error {
	if len(problems) == 0 {
		return nil
	}

	diagnostics := make([]report.Diagnostic, len(problems))
	for index := range problems {
		diagnostics[index] = problems[index].diagnostic
	}

	return p.stopAll(combine(problems), diagnostics)
}

// registerSources records every distinct source in the file registry, so that no source is also the
// output, the report, the plan or a font. Once every file is recorded the inventory is complete and the
// destination policy of the report is known.
func (p *pipeline) registerSources() error {
	var aliases, unreadable []problem

	for index := range p.flat.Files {
		file := &p.flat.Files[index]

		_, err := p.registry.Add(capture.RoleSource, file.Path)
		if err == nil {
			continue
		}

		found := classify(stageInput, err)
		found.diagnostic.Path = file.Path
		found.diagnostic.Location = p.at(file.FirstUse, "")

		if found.diagnostic.Code == codeAliasConflict {
			aliases = append(aliases, found)
		} else {
			unreadable = append(unreadable, found)
		}
	}

	if len(aliases) > 0 {
		return p.stopProblems(append(aliases, unreadable...))
	}

	if len(unreadable) > 0 {
		p.phases.Instructions = report.PhaseComplete
		p.phases.InputInspection = report.PhaseIncomplete

		return p.stopProblems(unreadable)
	}

	p.inventoryComplete = true

	return p.preflightReport()
}

// inspectSources captures every distinct source into the workspace and inspects the copy, with at most
// --jobs workers and never more workers than sources. Results are stored by position, and diagnostics are
// ordered by first use, so the outcome does not depend on completion order.
func (p *pipeline) inspectSources(ctx context.Context) error {
	err := p.startEngine()
	if err != nil {
		return err
	}

	p.progress.enter(stageInspect)

	files := p.flat.Files
	failures := make([]*problem, len(files))

	var (
		queue = make(chan int)
		done  atomic.Int64
		group sync.WaitGroup
	)

	for range p.jobCount(len(files)) {
		group.Go(func() {
			for index := range queue {
				failures[index] = p.inspectOne(ctx, index)
				p.progress.step(int(done.Add(1)), len(files))
			}
		})
	}

	feedSourceIndexes(ctx, queue, len(files))

	group.Wait()

	err = p.cancelled(ctx, stageInput)
	if err != nil {
		return err
	}

	return p.stopInspection(failures)
}

// stopInspection records the failures of the inspection workers in input order.
func (p *pipeline) stopInspection(failures []*problem) error {
	problems := make([]problem, 0, len(failures))

	for index, found := range failures {
		if found == nil {
			continue
		}

		p.builder.AddDiagnostic(index, found.diagnostic)

		problems = append(problems, *found)
	}

	if len(problems) == 0 {
		return nil
	}

	p.status = combine(problems)

	return errStopped
}

// inspectOne captures and inspects one distinct source; it returns the problem, or nil.
func (p *pipeline) inspectOne(ctx context.Context, index int) *problem {
	file := &p.flat.Files[index]

	captured, err := p.captures.Capture(ctx, file.Path)
	if err == nil {
		var info pdfengine.SourceInfo

		info, err = p.pdf.Inspect(ctx, captured.Path)
		if err == nil {
			p.files[index] = inspectedSource{captured: captured, info: info}

			return nil
		}
	}

	found := classify(stageInput, err)
	found.diagnostic.Path = file.Path
	found.diagnostic.Location = p.at(file.FirstUse, "")

	return &found
}

// startEngine creates the PDF engine, recording a failure to do so.
func (p *pipeline) startEngine() error {
	created, err := p.app.newEngine()
	if err != nil {
		return p.stopWith(problem{
			diagnostic: report.Diagnostic{
				Stage:   stageInput,
				Code:    codeEngineStart,
				Message: "cannot initialize the PDF engine: " + err.Error(),
			},
			status: report.StatusFailed,
		})
	}

	p.pdf = created

	return nil
}

func fontDeclarationOrigin(flat *assembly.Flattened, contribution *assembly.Contribution) assembly.Origin {
	if contribution.TextOverrides != nil && contribution.TextOverrides.Font.IsSet() {
		return contribution.TextOverrides.Font.Origin
	}

	if flat.TextDefaults != nil && flat.TextDefaults.Font.IsSet() {
		return flat.TextDefaults.Font.Origin
	}

	return contribution.Origin
}

// feedSourceIndexes closes the queue when all sources are handed out or the caller cancels.
func feedSourceIndexes(ctx context.Context, queue chan<- int, count int) {
	defer close(queue)

	for index := range count {
		if ctx.Err() != nil {
			return
		}

		select {
		case queue <- index:
		case <-ctx.Done():
			return
		}
	}
}

// usesDefaultFont identifies whether any actual blank appearance needs the built-in font identity.
func (p *pipeline) usesDefaultFont() bool {
	for index := range p.flat.Styles {
		if p.flat.Styles[index].Style.Text.Font.Value.IsDefault() {
			return true
		}
	}

	return false
}
