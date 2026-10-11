// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

func TestProgressZeroCompletedCountIsObservedWithoutAdvancement(t *testing.T) {
	t.Parallel()
	sink := &progressCapture{}
	session := NewProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, sink)
	t.Cleanup(session.Stop)
	session.Observe(observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources})
	session.Stop()
	if !session.snapshot().lastAdvance.IsZero() {
		t.Fatal("observing zero completed work invented advancement")
	}
	records := decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 2 || records[0].Event != "phase" || records[0].Phase != "input_inspection" ||
		records[1].Event != "counter" || records[1].Unit != "processed_sources" ||
		records[1].Completed == nil || *records[1].Completed != 0 || records[1].Total != nil || records[1].WorkAgeMS != nil {
		t.Fatalf("zero completed work was not retained truthfully: %+v", records)
	}
}

func TestProgressFinalUnknownTotalRemainsAbsent(t *testing.T) {
	t.Parallel()
	sink := &progressCapture{}
	session := NewProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, sink)
	t.Cleanup(session.Stop)
	session.Observe(observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: 1})
	session.Stop()
	seen := false
	for _, record := range decodeProgressRecords(t, sink.recordsCopy()) {
		if record.Event != "counter" {
			continue
		}
		seen = true
		if record.Unit != observation.ProcessedSources.String() || record.Completed == nil || *record.Completed != 1 || record.Total != nil {
			t.Fatalf("final unknown-total counter changed meaning: %+v", record)
		}
	}
	if !seen {
		t.Fatal("final unknown-total counter missing")
	}
}

func TestProgressLateKnownTotalDoesNotInventWork(t *testing.T) {
	t.Parallel()
	began := time.Unix(1, 0)
	now := began
	sink := &progressCapture{}
	controller := &Progress{mode: cli.ProgressJSON, attempt: progressTestAttempt, writer: sink, now: func() time.Time { return now }}
	state := progressState{began: began}
	milestone := observation.Milestone{Phase: observation.InputInspection, Unit: observation.ProcessedSources, Completed: 1, Total: 99}
	state.observe(milestone, began)
	emission := progressEmission{}
	if !emission.drain(t.Context(), controller, &state) {
		t.Fatal("initial unknown-total observation failed")
	}
	records := decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 2 || records[1].Event != "counter" || records[1].Total != nil {
		t.Fatal("unknown total payload became a denominator")
	}
	now = began.Add(100 * time.Millisecond)
	milestone.Total, milestone.TotalKnown = 3, true
	if !state.observe(milestone, now) || !emission.drain(t.Context(), controller, &state) {
		t.Fatal("late-known denominator was not presented")
	}
	records = decodeProgressRecords(t, sink.recordsCopy())
	if len(records) != 3 || records[2].Total == nil || *records[2].Total != 3 || records[2].Completed == nil || *records[2].Completed != 1 {
		t.Fatal("late-known total changed the actual completed count")
	}
	if !state.lastAdvance.Equal(began) || records[2].WorkAgeMS == nil || *records[2].WorkAgeMS != 100 {
		t.Fatal("learning a denominator invented work advancement")
	}
}
