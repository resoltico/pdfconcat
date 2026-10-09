// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package observation_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/observation"
)

type milestoneRecorder struct{ received []observation.Milestone }

func TestUnknownMilestoneNamesDoNotInventWorkLabels(t *testing.T) {
	t.Parallel()

	for _, phase := range []observation.Phase{observation.PhaseCount, observation.Phase(255)} {
		if phase.String() != "unknown" {
			t.Fatal("unknown phase invented a known phase label")
		}
	}

	for _, unit := range []observation.Unit{observation.UnitNone, observation.UnitCount, observation.Unit(255)} {
		if unit.String() != "" {
			t.Fatal("indeterminate/invalid unit invented a counter label")
		}
	}
}

func TestEmitWithoutObserverDoesNotRequireObservation(t *testing.T) {
	t.Parallel()

	observation.Emit(nil, observation.Milestone{Phase: observation.Preparation, Advance: true})
}

func TestEmitForwardsExactlyOneUnchangedWorkMilestone(t *testing.T) {
	t.Parallel()

	milestone := observation.Milestone{
		Phase: observation.Assembly, Unit: observation.CompiledPages,
		Completed: 3, Total: 8, TotalKnown: true, Advance: true,
	}
	observer := &milestoneRecorder{}
	observation.Emit(observer, milestone)

	if len(observer.received) != 1 || observer.received[0] != milestone {
		t.Fatalf("work milestone changed or repeated: %+v", observer.received)
	}
}

func (recorder *milestoneRecorder) Observe(milestone observation.Milestone) {
	recorder.received = append(recorder.received, milestone)
}
