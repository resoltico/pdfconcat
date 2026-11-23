// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package result

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"slices"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
)

type collectingWriter struct {
	records *int
	firstAt int
	largest int
	total   int
}

func (w *collectingWriter) Write(b []byte) (int, error) {
	if w.total == 0 {
		w.firstAt = *w.records
	}
	w.largest = max(w.largest, len(b))
	w.total += len(b)
	return len(b), nil
}

func TestFullReportStreamsParts(t *testing.T) {
	const count = 30_000
	produced := 0
	sink := collectingWriter{records: &produced}
	parts := func(yield func(outcome.Part) bool) {
		for i := range count {
			produced++
			if !yield(outcome.Part{Item: fmt.Sprintf("/items/%d", i), Kind: packet.PartPage, Count: 1}) {
				return
			}
		}
	}
	sections := func(func(outcome.Section) bool) {}
	if err := WriteReport(&sink, outcome.New("check"), parts, sections); err != nil {
		t.Fatal(err)
	}
	if produced != count || sink.firstAt >= 100 || sink.largest > 64<<10 || sink.total < 1<<20 {
		t.Fatalf("report did not stream bounded records: last=%d first-write=%d largest-write=%d bytes=%d", produced, sink.firstAt, sink.largest, sink.total)
	}
}

func TestSignatureDiscardAtDiagnosticRecordCap(t *testing.T) {
	const limit = outcome.SummaryLimit
	want := SignatureDiscardCounts{PopulatedSignatureFields: 1, Widgets: 1, DocumentTimestamps: 2, SignatureFlagEntries: 1}
	for _, tc := range []struct {
		name string
		slot int
	}{
		{"last included diagnostic", limit - 1},
		{"first omitted diagnostic", limit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := outcome.New("check")
			for i := range limit + 1 {
				finding := diag.Report(diag.SourceViewerSettingsRemoved, diag.SourceViewerSettingsAreOmittedFromComposedOutput())
				if i == tc.slot {
					finding = diag.SignatureDiscarded(diag.SignatureDiscardCounts{
						PopulatedSignatureFields: 1, Widgets: 1, DocumentTimestamps: 2, SignatureFlagEntries: 1,
					})
				}
				finding.File = fmt.Sprintf("source-%d.pdf", i)
				r.Diagnostics = append(r.Diagnostics, finding)
			}
			r = r.Full()
			summary, err := json.Marshal(ProjectSummary(r))
			if err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			if err := WriteReport(&report, r, slices.Values(r.Parts), slices.Values(r.Sections)); err != nil {
				t.Fatal(err)
			}
			for _, view := range []struct {
				name             string
				data             []byte
				records, omitted int
			}{{"summary", summary, limit, 1}, {"report", report.Bytes(), limit + 1, 0}} {
				var value Result
				if err := json.Unmarshal(view.data, &value); err != nil {
					t.Fatal(err)
				}
				if len(value.Diagnostics) != view.records || value.DiagnosticsOmitted != view.omitted {
					t.Fatalf("%s has %d diagnostics, %d omitted", view.name, len(value.Diagnostics), value.DiagnosticsOmitted)
				}
				slot := slices.IndexFunc(value.Diagnostics, func(d Diagnostic) bool { return d.Code == diag.SourceSignatureDiscarded })
				if tc.slot >= view.records {
					if slot != -1 {
						t.Fatalf("%s included discard outside the cap at %d", view.name, slot)
					}
					continue
				}
				if slot != tc.slot {
					t.Fatalf("%s discard position %d, want %d", view.name, slot, tc.slot)
				}
				if counts := value.Diagnostics[slot].SignatureDiscard; counts == nil || *counts != want {
					t.Fatalf("%s signature discard counts: %+v, want %+v", view.name, counts, want)
				}
			}
		})
	}
}
