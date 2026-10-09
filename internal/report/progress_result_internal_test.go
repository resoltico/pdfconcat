// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"bytes"
	"testing"
)

const interruptedReportFile = "interrupted.json"

func TestFinalInterruptionAnnotatesCopyAndSavedQueryRetainsFact(t *testing.T) {
	t.Parallel()

	captured := capturedFitReport()
	response := NewResponse(captured).WithProgressInterrupted()

	final, ok := ContentOf[Report](response)
	if !ok || !final.ProgressInterrupted || captured.ProgressInterrupted {
		t.Fatal("final transport annotation mutated captured report")
	}

	var saved bytes.Buffer
	if _, err := Write(&saved, final, MaxReportBytes); err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(t.Context(), interruptedReportFile, bytes.NewReader(saved.Bytes()))
	if err != nil || !decoded.ProgressInterrupted || !decoded.Summary().ProgressInterrupted {
		t.Fatalf("saved interruption lost: %v", err)
	}

	encoded, err := Encode(NewResponse(captured.Summary()).WithProgressInterrupted())
	if err != nil || len(encoded)+1 > SummaryBytes {
		t.Fatalf("interrupted summary exceeded budget: %d %v", len(encoded)+1, err)
	}
}

func TestInterruptionUsesOrdinarySavedNodeAndByteLimits(t *testing.T) {
	t.Parallel()

	final := capturedFitReport()
	final.ProgressInterrupted = true

	var saved bytes.Buffer
	if _, writeErr := Write(&saved, final, MaxReportBytes); writeErr != nil {
		t.Fatal(writeErr)
	}

	limits := DefaultLimits()

	limits.MaxNodes = final.nodes()
	if _, err := DecodeLimited(t.Context(), interruptedReportFile, bytes.NewReader(saved.Bytes()), limits); err != nil {
		t.Fatal(err)
	}

	limits.MaxNodes--
	if _, err := DecodeLimited(t.Context(), interruptedReportFile, bytes.NewReader(saved.Bytes()), limits); err == nil {
		t.Fatal("extra interruption node escaped budget")
	}

	limits = DefaultLimits()

	limits.MaxBytes = int64(saved.Len()) - 1
	if _, err := DecodeLimited(t.Context(), interruptedReportFile, bytes.NewReader(saved.Bytes()), limits); err == nil {
		t.Fatal("interruption bytes escaped saved-file budget")
	}
}
