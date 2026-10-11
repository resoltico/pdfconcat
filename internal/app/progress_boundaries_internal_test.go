// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

func TestProgressCounterUpdatesWakePresentation(t *testing.T) {
	t.Parallel()
	controller := &Progress{wake: make(chan struct{}, 1), now: func() time.Time { return time.Unix(1, 0) }}
	milestone := observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: 1}
	controller.Observe(milestone)
	select {
	case <-controller.wake:
	default:
		t.Fatal("first observation did not wake presentation")
	}
	milestone.Completed = 2
	controller.Observe(milestone)
	select {
	case <-controller.wake:
	default:
		t.Fatal("existing-phase count advancement did not wake presentation")
	}
	controller.Observe(milestone)
	select {
	case <-controller.wake:
		t.Fatal("duplicate count woke presentation")
	default:
	}
}

func TestProgressObserverRejectsTerminalEnumsAndStoppedUpdates(t *testing.T) {
	t.Parallel()
	for _, entry := range []struct {
		name      string
		milestone observation.Milestone
		stopping  bool
	}{
		{name: "stopped", milestone: observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: 1}, stopping: true},
		{name: "terminal phase", milestone: observation.Milestone{Phase: observation.PhaseCount}},
		{name: "terminal unit", milestone: observation.Milestone{Phase: observation.InputInspection, Unit: observation.UnitCount}},
		{name: "negative count", milestone: observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: -1}},
		{name: "count beyond known total", milestone: observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: 2, Total: 1, TotalKnown: true}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			controller := &Progress{wake: make(chan struct{}, 1), now: func() time.Time { return time.Unix(1, 0) }}
			controller.state.stopping = entry.stopping
			before := controller.snapshot()
			if controller.state.accepts(entry.milestone) {
				t.Fatal("invalid or stopped observation admitted")
			}
			controller.Observe(entry.milestone)
			if controller.snapshot() != before {
				t.Fatal("rejected observation changed retained state")
			}
			select {
			case <-controller.wake:
				t.Fatal("rejected observation woke presentation")
			default:
			}
		})
	}
}

func TestProgressRedrawAndHeartbeatUseIndependentTimeBoundaries(t *testing.T) {
	t.Parallel()
	began := time.Unix(1, 0)
	now := began
	sink := &progressCapture{}
	controller := &Progress{mode: cli.ProgressJSON, attempt: progressTestAttempt, writer: sink, now: func() time.Time { return now }}
	state := progressState{began: began}
	milestone := observation.Milestone{Phase: observation.Assembly, Unit: observation.CompiledPages, Completed: 1}
	state.observe(milestone, began)
	emission := progressEmission{}
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("initial observation failed")
	}
	initial := len(sink.recordsCopy())
	milestone.Completed = 2
	state.observe(milestone, began)
	now = began.Add(99 * time.Millisecond)
	if !emission.drain(t.Context(), controller, &state) || len(sink.recordsCopy()) != initial {
		t.Fatal("counter redrawn before 100 milliseconds")
	}
	now = began.Add(100 * time.Millisecond)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("due counter failed")
	}
	records := decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != initial+1 || records[len(records)-1].Completed == nil || *records[len(records)-1].Completed != 2 {
		t.Fatal("counter missing at 100 milliseconds")
	}
	// Liveness measures silence since the last emitted record, not work advancement.
	lastEmission := now
	now = lastEmission.Add(9999 * time.Millisecond)
	if !emission.drain(t.Context(), controller, &state) || len(sink.recordsCopy()) != len(records) {
		t.Fatal("heartbeat emitted before ten seconds of silence")
	}
	now = lastEmission.Add(10000 * time.Millisecond)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("due heartbeat failed")
	}
	after := decodeProgressRecords(t, sink.recordsCopy())
	if len(after) != len(records)+1 || after[len(after)-1].Event != "liveness" || after[len(after)-1].WorkAgeMS == nil || *after[len(after)-1].WorkAgeMS != 10100 {
		t.Fatal("ten-second heartbeat missing or changed actual work age")
	}
}
