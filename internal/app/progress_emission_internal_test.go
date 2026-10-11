// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

func TestProgressRetiredCountersFlushAndPreserveActiveRedrawBoundary(t *testing.T) {
	t.Parallel()
	began := time.Unix(1, 0)
	now := began
	sink := &progressCapture{}
	controller := &Progress{mode: cli.ProgressJSON, attempt: progressTestAttempt, writer: sink, now: func() time.Time { return now }}
	state := progressState{began: began}
	milestone := observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: 1}
	state.observe(milestone, now)
	emission := progressEmission{}
	if !emission.drain(t.Context(), controller, &state) || len(sink.recordsCopy()) != 2 {
		t.Fatal("initial phase and counter were not emitted")
	}

	now = began.Add(50 * time.Millisecond)
	milestone.Completed = 2
	state.observe(milestone, now)
	state.observe(observation.Milestone{Phase: observation.Layout}, now)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("phase transition failed")
	}
	records := decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 4 || records[2].Event != "counter" || records[2].Phase != "input_inspection" ||
		records[2].Unit != "processed_sources" || records[2].Completed == nil || *records[2].Completed != 2 ||
		records[3].Event != "phase" || records[3].Phase != "layout" || records[3].Unit != "" || records[3].Completed != nil {
		t.Fatalf("retired phase counter was not flushed before entering layout: %+v", records)
	}

	now = began.Add(75 * time.Millisecond)
	state.observe(observation.Milestone{Phase: observation.Layout, Unit: observation.ProcessedGeneratedSpecs, Completed: 1}, now)
	for _, milliseconds := range []int{75, 149} {
		now = began.Add(time.Duration(milliseconds) * time.Millisecond)
		if !emission.drain(t.Context(), controller, &state) || len(sink.recordsCopy()) != 4 {
			t.Fatalf("active counter emitted at %dms before its global redraw boundary", milliseconds)
		}
	}
	now = began.Add(150 * time.Millisecond)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("active redraw failed at its boundary")
	}
	records = decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 5 || records[4].Event != "counter" || records[4].Phase != "layout" ||
		records[4].Unit != "processed_generated_specs" || records[4].Completed == nil || *records[4].Completed != 1 {
		t.Fatalf("active counter missing at 100ms since the retired counter emission: %+v", records)
	}
}

func TestProgressFreshCounterRestartsHeartbeatSilence(t *testing.T) {
	t.Parallel()
	began := time.Unix(1, 0)
	now := began
	sink := &progressCapture{}
	controller := &Progress{mode: cli.ProgressJSON, attempt: progressTestAttempt, writer: sink, now: func() time.Time { return now }}
	state := progressState{began: began}
	milestone := observation.Milestone{Phase: observation.Assembly, Unit: observation.CompiledPages, Completed: 1}
	state.observe(milestone, now)
	emission := progressEmission{}
	if !emission.drain(t.Context(), controller, &state) || len(sink.recordsCopy()) != 2 {
		t.Fatal("initial progress was not emitted")
	}
	now = began.Add(10 * time.Second)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("quiet heartbeat failed")
	}
	records := decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 3 || records[2].Event != "liveness" || records[2].Phase != "assembly" {
		t.Fatalf("ten seconds of silence did not emit one heartbeat: %+v", records)
	}

	now = began.Add(20 * time.Second)
	milestone.Completed = 2
	state.observe(milestone, now)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("fresh counter failed")
	}
	records = decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 4 || records[3].Event != "counter" || records[3].Phase != "assembly" ||
		records[3].Unit != "compiled_pages" || records[3].Completed == nil || *records[3].Completed != 2 {
		t.Fatalf("fresh work emitted a simultaneous heartbeat or lost its counter: %+v", records)
	}
	now = began.Add(30 * time.Second)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("heartbeat after fresh work failed")
	}
	records = decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 5 || records[4].Event != "liveness" || records[4].Phase != "assembly" ||
		records[4].WorkAgeMS == nil || *records[4].WorkAgeMS != 10000 {
		t.Fatalf("new silence interval did not preserve actual work age: %+v", records)
	}
	now = began.Add(39999 * time.Millisecond)
	if !emission.drain(t.Context(), controller, &state) || len(sink.recordsCopy()) != 5 {
		t.Fatal("repeated heartbeat emitted before ten seconds of silence")
	}
	now = began.Add(40 * time.Second)
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("repeated heartbeat failed at its boundary")
	}
	records = decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 6 || records[5].Event != "liveness" || records[5].Phase != "assembly" ||
		records[5].WorkAgeMS == nil || *records[5].WorkAgeMS != 20000 {
		t.Fatalf("repeated heartbeat missing at ten seconds since the preceding heartbeat: %+v", records)
	}
}

func TestProgressHeartbeatOnlyEmitsForRunningJSONPresentation(t *testing.T) {
	t.Parallel()
	for _, entry := range []struct {
		name          string
		mode          cli.ProgressMode
		stopping      bool
		wantHeartbeat bool
	}{
		{"running JSON", cli.ProgressJSON, false, true},
		{"stopped JSON", cli.ProgressJSON, true, false},
		{"running human", cli.ProgressAuto, false, false},
		{"stopped human", cli.ProgressAuto, true, false},
	} {
		t.Run(entry.name, func(t *testing.T) {
			t.Parallel()
			began := time.Unix(1, 0)
			now := began
			sink := &progressCapture{}
			controller := &Progress{mode: entry.mode, attempt: progressTestAttempt, writer: sink, now: func() time.Time { return now }}
			state := progressState{began: began}
			state.observe(observation.Milestone{Phase: observation.Assembly, Unit: observation.CompiledPages, Completed: 1, Total: 2, TotalKnown: true}, now)
			emission := progressEmission{}
			if !emission.drain(t.Context(), controller, &state) || len(sink.recordsCopy()) != 2 {
				t.Fatal("initial phase and counter were not emitted")
			}
			initial := sink.recordsCopy()
			initialWire := bytes.Join(initial, nil)
			if entry.mode == cli.ProgressJSON {
				records := decodeProgressRecords(t, initial)
				if records[0].Event != "phase" || records[0].Phase != "assembly" || records[0].Unit != "" || records[0].Completed != nil ||
					records[1].Event != "counter" || records[1].Phase != "assembly" || records[1].Unit != "compiled_pages" ||
					records[1].Completed == nil || *records[1].Completed != 1 || records[1].Total == nil || *records[1].Total != 2 {
					t.Fatalf("initial observations were not completely emitted: %+v", records)
				}
			} else if !bytes.Contains(initialWire, []byte("1/2 compiled pages")) {
				t.Fatalf("initial human observations were not emitted: %q", initialWire)
			}
			// Stop marks the snapshot before its final drain; it does not invent new work.
			state.stopping = entry.stopping
			now = began.Add(10 * time.Second)
			if !emission.drain(t.Context(), controller, &state) {
				t.Fatal("quiet presentation drain failed")
			}
			data := sink.recordsCopy()
			if !entry.wantHeartbeat {
				if len(data) != 2 || !bytes.Equal(bytes.Join(data, nil), initialWire) {
					t.Fatalf("quiet %s presentation changed its last complete output: %q", entry.name, data)
				}
				return
			}
			records := decodeProgressRecords(t, data)
			if len(records) != 3 || records[2].Event != "liveness" || records[2].Phase != "assembly" || records[2].Unit != "" ||
				records[2].Completed != nil || records[2].Total != nil || records[2].WorkAgeMS == nil || *records[2].WorkAgeMS != 10000 ||
				records[2].Sequence != 3 || state.phases[observation.Assembly].counters[observation.CompiledPages].completed != 1 {
				t.Fatalf("running JSON heartbeat lost its idle observation semantics: %+v", records)
			}
		})
	}
}
