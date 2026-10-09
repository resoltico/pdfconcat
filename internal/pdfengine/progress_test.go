// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/observation"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

type recordedMilestones struct{ values []observation.Milestone }

func (recorded *recordedMilestones) Observe(milestone observation.Milestone) {
	recorded.values = append(recorded.values, milestone)
}

func TestAssemblyMilestonesCountActualImportsAndPlacedPages(t *testing.T) {
	t.Parallel()
	world := newWorld(t)
	compiled := world.compile(filepath.Join(t.TempDir(), outputFilename), []part{whole(fixtureLinks), gen(0, 3), whole(fixtureLinks)})
	recorded := &recordedMilestones{}

	compiled.request.Observer = recorded
	if err := world.engine.Assemble(t.Context(), &compiled.request); err != nil {
		t.Fatal(err)
	}

	requireAssemblyMilestones(t, recorded, int64(compiled.request.ExpectedPages))

	document, err := pdforacle.Load(world.tools, compiled.request.Destination)
	if err != nil {
		t.Fatal(err)
	}

	requireClean(t, document, compiled.expectation)
}

func requireAssemblyMilestones(t *testing.T, recorded *recordedMilestones, expectedPages int64) {
	t.Helper()

	phases := []observation.Phase{}

	var imported, placed int64

	for _, milestone := range recorded.values {
		if len(phases) == 0 || phases[len(phases)-1] != milestone.Phase {
			phases = append(phases, milestone.Phase)
		}

		imported = requireMilestoneCounter(t, milestone, observation.ImportedSources, imported)
		placed = requireMilestoneCounter(t, milestone, observation.CompiledPages, placed)
	}

	want := []observation.Phase{observation.Assembly, observation.Optimization, observation.OutputWriting, observation.OutputVerification}
	if !slices.Equal(phases, want) {
		t.Fatalf("backend phases %v", phases)
	}

	if imported != 2 || placed != expectedPages {
		t.Fatalf("actual imports/pages: %d/%d", imported, placed)
	}
}

func requireMilestoneCounter(t *testing.T, milestone observation.Milestone, unit observation.Unit, previous int64) int64 {
	t.Helper()

	if milestone.Unit != unit {
		return previous
	}

	if milestone.Completed < previous {
		t.Fatal("counter regressed")
	}

	if unit == observation.ImportedSources && milestone.TotalKnown {
		t.Fatal("source import count invented denominator")
	}

	return milestone.Completed
}
