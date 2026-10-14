// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestQuerySingleCutLabelFitsWithoutLosingCapturedFacts(t *testing.T) {
	t.Parallel()
	saved := summaryRequiredFacts(t)
	saved.Diagnostics = []report.Diagnostic{saved.Diagnostics[0], saved.Diagnostics[2]}
	primary := &saved.Diagnostics[0]
	primary.Stage, primary.Code, primary.Message = report.StageWrite, report.CodeWriteFailed, "report failed"
	primary.Path, primary.Cause, primary.Location = "", strings.Repeat("a", 1000), nil
	primary.Recovery.Replacement = ""
	primary.Recovery.Location.File = "source.json"
	primary.Recovery.Location.Pointer = "/items/123"
	primary.Recovery.LocationFrom = "complete_report.diagnostics/0/recovery/location"
	saved.Publication.Output, saved.Publication.ReportPath, saved.Publication.RecoveryReport = "o", "r", "recovery.json"
	saved.FinalizeDiagnostics()
	saved = mustDecode(t, encodeReport(t, saved))
	before := encodeReport(t, saved)
	// The current caller operand is independently valid and uses short directory
	// components. Its exact argv exceeds the preview, so caller-owned reference
	// navigation is required, independently of the historical publication paths.
	operand := strings.Repeat("d/", 128) + "saved.json"
	query := report.QueryResultOf(saved, queryOK(t, saved, report.Request{}), programName, operand)
	payload, err := report.Encode(query)
	var human strings.Builder
	textErr := query.RenderText(&human)
	if err != nil || len(payload)+1 != 2048 || textErr != nil || human.Len() > 2048 {
		t.Fatalf("single-label query: JSON%d text%d error%v", len(payload)+1, human.Len(), err)
	}
	if err = query.Validate(); err != nil {
		t.Fatalf("truthful fitting preview rejected: %v", err)
	}
	brief, found := report.ContentOf[report.Summary](query.Result)
	if !found || len(brief.Diagnostics) != 1 || brief.DiagnosticsOmitted != 1 || !reflect.DeepEqual(brief.TruncatedFields, []string{"diagnostics/0"}) {
		t.Fatal("single loss marker changed visible records or was not compacted")
	}
	got := brief.Diagnostics[0]
	want := saved.Diagnostics[0]
	if got.Stage != want.Stage || got.Code != want.Code || got.Message != want.Message || got.MessageTruncated || !reflect.DeepEqual(got.Recovery, want.Recovery) || !reflect.DeepEqual(brief.Counts, saved.Counts) {
		t.Fatal("label compaction discarded retained message, code, remedy or page counts")
	}
	if !reflect.DeepEqual(brief.Publication, saved.Publication) || brief.Phases != saved.Phases || brief.AttemptID != saved.AttemptID || brief.Status != saved.Status {
		t.Fatal("label compaction changed captured publication or outcome")
	}
	if !brief.NextOmitted || brief.Next != nil || brief.NextReference == nil || query.DiagnosticsReference == nil || brief.NextReference.AttemptID != saved.AttemptID || query.SavedRun.WarningCount != 1 || query.SelectionCounts.WarningCount != 1 {
		t.Fatal("label compaction lost caller-owned or warning navigation")
	}
	if encodeReport(t, saved) != before {
		t.Fatal("summary changed complete evidence")
	}
	// Change only the serialized marker to measure the former representation.
	// No manually constructed Summary or private budget is supplied to production.
	leaf := bytes.Replace(payload, []byte(`"truncated_fields":["diagnostics/0"]`), []byte(`"truncated_fields":["diagnostics/0/cause"]`), 1)
	if len(leaf)+1 != 2054 {
		t.Fatalf("independent leaf-marker measurement=%d", len(leaf)+1)
	}
	if schemaVerdict(t, compileSchema(t, report.ResponseSchema(), responseSchemaURL), payload) != schemaAccept {
		t.Fatal("compacted response violates schema")
	}
	t.Logf("actual JSON%d text%d; same facts with leaf marker JSON%d", len(payload)+1, human.Len(), len(leaf)+1)
	// A shorter complete recovery filename makes the leaf-level metadata fit.
	// Keep its more precise cut label instead of compacting an already fitting result.
	saved.Diagnostics[0].Recovery.Location.File = "f"
	saved = mustDecode(t, encodeReport(t, saved))
	fitting := report.QueryResultOf(saved, queryOK(t, saved, report.Request{}), programName, operand)
	precise, _ := report.ContentOf[report.Summary](fitting.Result)
	fittingBytes, fittingErr := report.Encode(fitting)
	if fittingErr != nil || len(fittingBytes)+1 != 2044 || fitting.Validate() != nil || !reflect.DeepEqual(precise.TruncatedFields, []string{"diagnostics/0/cause"}) {
		t.Fatal("already fitting leaf label lost its field granularity")
	}
}
