// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/observation"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

type phaseCancellation struct {
	cancel   context.CancelFunc
	phase    observation.Phase
	observed bool
}

func (trigger *phaseCancellation) Observe(milestone observation.Milestone) {
	if milestone.Phase == trigger.phase {
		trigger.observed = true
		trigger.cancel()
	}
}

func TestFittedBackendCancellationKeepsFailureIdentity(t *testing.T) {
	t.Parallel()

	for _, paper := range []assembly.FitTarget{assembly.FitA4, assembly.FitLegal} {
		for _, phase := range []observation.Phase{observation.Optimization, observation.OutputWriting, observation.OutputVerification} {
			t.Run(string(paper)+"/"+phase.String(), func(t *testing.T) {
				t.Parallel()
				assertFittedPhaseCancellation(t, paper, phase)
			})
		}
	}
}

func assertFittedPhaseCancellation(t *testing.T, paper assembly.FitTarget, phase observation.Phase) {
	t.Helper()
	world := newWorld(t)
	compiled := world.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA)})

	dim, err := paper.Dim()
	if err != nil {
		t.Fatal(err)
	}

	compiled.request.FitTarget = &pdfengine.PageSize{Width: float64(dim.Width), Height: float64(dim.Height)}
	for index := range compiled.request.Sources {
		info, inspectErr := world.engine.Inspect(t.Context(), compiled.request.Sources[index].Path, compiled.request.FitTarget)
		if inspectErr != nil {
			t.Fatal(inspectErr)
		}

		compiled.request.Sources[index].Info = info
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	trigger := &phaseCancellation{cancel: cancel, phase: phase}
	compiled.request.Observer = trigger
	err = world.engine.Assemble(ctx, &compiled.request)

	failure := requireFailure(t, err, pdfengine.CodeCanceled)
	if !trigger.observed || !errors.Is(failure, context.Canceled) || compiled.request.OutputDigest != "" {
		t.Fatalf(
			"fitted backend cancellation: observed=%t digest=%q error=%v",
			trigger.observed,
			compiled.request.OutputDigest,
			err,
		)
	}
}
