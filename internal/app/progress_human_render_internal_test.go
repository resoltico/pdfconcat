// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

func TestProgressHumanClearsOnlyThePreviousLineCells(t *testing.T) {
	t.Parallel()
	now := time.Unix(1, 0)
	sink := &progressCapture{}
	controller := &Progress{mode: cli.ProgressAuto, writer: sink, now: func() time.Time { return now }}
	state := progressState{began: now}
	state.observe(observation.Milestone{
		Phase: observation.InputInspection, Unit: observation.ProcessedSources,
		Completed: 12345, Total: 12345, TotalKnown: true,
	}, now)
	emission := progressEmission{}
	if !emission.record(t.Context(), controller, &state, observation.InputInspection, observation.ProcessedSources, "counter", now) {
		t.Fatal("human counter write failed")
	}
	state.observe(observation.Milestone{Phase: observation.Finalization}, now)
	if !emission.record(t.Context(), controller, &state, observation.Finalization, observation.UnitNone, "phase", now) {
		t.Fatal("shorter human phase write failed")
	}
	emission.erase(t.Context(), controller)
	if controller.Interrupted() {
		t.Fatal("successful human rendering or erase poisoned telemetry")
	}

	// These fixed ASCII lines occupy 57 and 23 cells; the stale tail is 34 cells.
	longLine := "pdfconcat: input inspection 12345/12345 processed sources"
	shortLine := "pdfconcat: finalization"
	expected := []string{
		"\r" + longLine + "\r" + longLine,
		"\r" + shortLine + strings.Repeat(" ", 34) + "\r" + shortLine,
		"\r" + strings.Repeat(" ", 57) + "\r",
	}
	records := sink.recordsCopy()
	if len(records) != len(expected) {
		t.Fatalf("human line completion wrote %d records, want three", len(records))
	}
	for index := range expected {
		if string(records[index]) != expected[index] {
			t.Fatalf("human record %d changed occupied terminal cells: %q", index, records[index])
		}
	}
}

func TestProgressJoinedEmptyHumanAndObservedJSONKeepOutputBoundaries(t *testing.T) {
	t.Parallel()
	clock := func() time.Time { return time.Unix(1, 0) }
	empty := &progressCapture{}
	human := startProgress(t.Context(), cli.ProgressAuto, progressTestAttempt, empty, clock, nil, func() {})
	human.Stop()
	if len(empty.recordsCopy()) != 0 {
		t.Fatal("empty joined human presentation changed terminal cells")
	}

	sink := &progressCapture{}
	json := startProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, sink, clock, nil, func() {})
	json.Observe(observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: 1})
	json.Stop()
	wire := sink.recordsCopy()
	if bytes.Contains(bytes.Join(wire, nil), []byte{'\r'}) {
		t.Fatal("joined JSON presentation emitted human cursor control")
	}
	records := decodeProgressRecords(t, wire)
	if len(records) != 2 || records[0].Event != "phase" || records[0].Phase != "input_inspection" ||
		records[1].Event != "counter" || records[1].Phase != "input_inspection" || records[1].Unit != "processed_sources" ||
		records[1].Completed == nil || *records[1].Completed != 1 {
		t.Fatalf("joined JSON presentation lost retained observations: %+v", records)
	}
}
