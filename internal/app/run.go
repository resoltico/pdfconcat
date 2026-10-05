// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/publish"
)

// probePageDim is the page size used to resolve blank styles before the real, inherited size is known.
func probePageDim() assembly.PageDim {
	const widthPoints, heightPoints = 595, 842

	return assembly.PageDim{Width: widthPoints, Height: heightPoints}
}

// Run resolves, preflights, assembles, verifies, and publishes one request.
func (r *Runner) Run(ctx context.Context, req cli.Request) error {
	resolved, err := r.resolve(&req)
	if err != nil {
		return err
	}

	err = rejectOutputAsInput(&resolved.sequence, resolved.output)
	if err != nil {
		return err
	}

	err = publish.CheckDestination(resolved.output, req.Overwrite)
	if err != nil {
		return err
	}

	err = r.validateBlankStyles(ctx, &resolved)
	if err != nil {
		return err
	}

	documents, err := r.inspectAll(ctx, resolved.sequence.DistinctPDFPaths())
	if err != nil {
		return err
	}

	layout, err := assembly.BuildLayout(resolved.sequence, resolved.defaults, documents)
	if err != nil {
		return err
	}

	if req.DryRun {
		return r.report(newDryRunReport(resolved.output, &layout), req.JSON)
	}

	err = r.assembleAndPublish(ctx, resolved.output, &layout, req.Overwrite)
	if err != nil {
		return err
	}

	return r.report(newCreatedReport(resolved.output, &layout), req.JSON)
}

// validateBlankStyles resolves every distinct blank style before any PDF is read, so a
// bad color or unrenderable character fails in milliseconds rather than after validation
// of thousands of PDFs.
func (r *Runner) validateBlankStyles(ctx context.Context, resolved *job) error {
	checked := make(map[assembly.BlankSpec]struct{})

	for index := range resolved.sequence.Items {
		item := &resolved.sequence.Items[index]
		if item.Kind != assembly.Blank {
			continue
		}

		spec, err := item.Blank.Over(resolved.defaults).Resolve(probePageDim())
		if err != nil {
			return fmt.Errorf("sequence item %d (blank): %w", index+1, err)
		}

		if _, found := checked[spec]; found {
			continue
		}

		checked[spec] = struct{}{}

		err = r.engine.ValidateBlank(ctx, spec)
		if err != nil {
			return fmt.Errorf("sequence item %d (blank): %w", index+1, err)
		}
	}

	return nil
}

// assembleAndPublish assembles into a destination-local workspace, verifies, then publishes.
func (r *Runner) assembleAndPublish(ctx context.Context, output string, layout *assembly.Layout, overwrite bool) error {
	workspace, err := os.MkdirTemp(filepath.Dir(output), ".pdfconcat-")
	if err != nil {
		return fmt.Errorf("create temporary workspace: %w", err)
	}

	err = r.stageVerifyPublish(ctx, workspace, output, layout, overwrite)

	cleanupErr := os.RemoveAll(workspace)
	if cleanupErr != nil {
		r.warnf("could not remove temporary workspace %s: %v", workspace, cleanupErr)
	}

	return err
}

func (r *Runner) stageVerifyPublish(ctx context.Context, workspace, output string, layout *assembly.Layout, overwrite bool) error {
	staged := filepath.Join(workspace, "assembled.pdf")

	err := r.engine.Assemble(ctx, *layout, workspace, staged)
	if err != nil {
		return fmt.Errorf("assemble: %w", err)
	}

	err = r.verify(ctx, staged, layout.TotalPages)
	if err != nil {
		return err
	}

	err = ctx.Err()
	if err != nil {
		return fmt.Errorf("assembly interrupted: %w", err)
	}

	return publish.File(staged, output, overwrite)
}

func (r *Runner) verify(ctx context.Context, staged string, expectedPages int) error {
	info, err := r.engine.Inspect(ctx, staged)
	if err != nil {
		return fmt.Errorf("verify assembled output: %w", err)
	}

	if info.Pages != expectedPages {
		return fmt.Errorf("verify assembled page count: expected %d pages, got %d", expectedPages, info.Pages)
	}

	return nil
}

// warnf reports a non-fatal problem on the diagnostic stream, if there is one.
func (r *Runner) warnf(format string, args ...any) {
	if r.streams.Stderr == nil {
		return
	}

	// A failure to write a warning has nowhere further to be reported.
	_, writeErr := fmt.Fprintf(r.streams.Stderr, "pdfconcat: warning: "+format+"\n", args...)
	_ = writeErr
}
