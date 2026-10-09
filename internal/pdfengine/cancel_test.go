// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

// cancellationSweepLimit stops a sweep whose budget grows without completion.
const cancellationSweepLimit = 1 << 27

// TestAssembleCancellationAtEveryCheckpoint cancels after 0, 1, 2, ... context checks. Every outcome
// must be success or a CodeCanceled error that wraps [context.Canceled], never another failure.
func TestAssembleCancellationAtEveryCheckpoint(t *testing.T) {
	t.Parallel()

	env := newWorld(t)

	scenarios := map[string]struct {
		step  func(budget int64) int64
		parts []part
	}{
		"small": {func(b int64) int64 { return b + 1 }, []part{whole(fixturePlainA), gen(0, 2), whole(fixturePlainB)}},
		// More pages than the compile loop's check interval, so the periodic check is reached.
		"long run": {func(b int64) int64 {
			if b < 600 {
				return b + 1
			}

			return b*2 + 1
		}, []part{gen(0, 4100), whole(fixturePlainA)}},
	}

	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if canceledRuns := sweepAssembly(t, env, scenario.parts, scenario.step); canceledRuns == 0 {
				t.Fatal("no run was canceled")
			}
		})
	}
}

// sweepAssembly assembles parts with growing cancellation budgets until one completes, requires every
// earlier run to fail as canceled, and returns how many did.
func sweepAssembly(t *testing.T, env *world, parts []part, step func(budget int64) int64) int {
	t.Helper()

	canceledRuns := 0

	for budget := int64(0); ; budget = step(budget) {
		c := env.compile(filepath.Join(t.TempDir(), outputFilename), parts)

		err := env.engine.Assemble(pdfengine.CheckpointContext(t.Context(), t, budget), &c.request)
		if err == nil {
			return canceledRuns
		}

		failure := requireFailure(t, err, pdfengine.CodeCanceled)
		if !errors.Is(failure, context.Canceled) {
			t.Fatalf("budget %d: %v does not wrap context.Canceled", budget, err)
		}

		canceledRuns++

		if budget > cancellationSweepLimit {
			t.Fatal("never completed")
		}
	}
}

func TestAssembleCanceledBeforeStart(t *testing.T) {
	t.Parallel()

	env := newWorld(t)
	c := env.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixturePlainA)})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := env.engine.Assemble(ctx, &c.request)
	requireCode(t, err, pdfengine.CodeCanceled)
}

// TestInspectCancellationAtEveryCheckpoint is the Inspect counterpart of the assembly sweep.
func TestInspectCancellationAtEveryCheckpoint(t *testing.T) {
	t.Parallel()

	env := newWorld(t)
	canceledRuns := 0

	for budget := int64(0); ; budget++ {
		_, err := env.engine.Inspect(pdfengine.CheckpointContext(t.Context(), t, budget), env.files[fixtureMulti].Path, nil)
		if err == nil {
			break
		}

		requireCode(t, err, pdfengine.CodeCanceled)

		canceledRuns++
	}

	if canceledRuns == 0 {
		t.Fatal("no run was canceled")
	}
}
