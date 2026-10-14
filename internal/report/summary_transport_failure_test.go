// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func summaryRequiredFacts(tb testing.TB) *report.Report {
	tb.Helper()
	data, err := os.ReadFile("testdata/summary_required_facts.json")
	if err != nil {
		tb.Fatal(err)
	}
	if schemaVerdict(tb, reportSchema(tb), data) != schemaAccept {
		tb.Fatal("required-facts fixture violates report schema")
	}
	return mustDecode(tb, string(data))
}

func TestQuerySummaryRejectsUnrepresentableRequiredFacts(t *testing.T) {
	t.Parallel()
	saved := summaryRequiredFacts(t)
	before := encodeReport(t, saved)
	query := report.QueryResultOf(saved, queryOK(t, saved, report.Request{}), programName, fixtureReportPath)
	failure, ok := report.AsError(query.Validate())
	if !ok || failure.Status() != report.StatusFailed || failure.Diagnostic.Stage != report.StageLimit || failure.Diagnostic.Code != report.CodeResponseTooLarge {
		t.Fatalf("oversized required facts accepted or misclassified: %v", failure)
	}
	brief, ok := report.ContentOf[report.Summary](query.Result)
	if !ok || len(brief.Diagnostics) != 2 || query.DiagnosticsReference == nil || brief.NextReference == nil {
		t.Fatal("projection concealed required safety or navigation")
	}
	for i := range brief.Diagnostics {
		if brief.Diagnostics[i].Code != saved.Diagnostics[i].Code || !reflect.DeepEqual(brief.Diagnostics[i].Recovery, saved.Diagnostics[i].Recovery) {
			t.Fatal("projection changed required code or remedy")
		}
	}
	if encodeReport(t, saved) != before {
		t.Fatal("failed summary changed complete evidence")
	}
	assertBoundedSummaryRejection(t, saved, failure, compileSchema(t, report.ResponseSchema(), responseSchemaURL))
	for _, view := range []string{report.ViewDiagnostics, report.ViewParts} {
		paged := report.QueryResultOf(saved, queryOK(t, saved, report.Request{View: view, Details: true}), programName, fixtureReportPath)
		if err := paged.Validate(); err != nil {
			t.Fatalf("summary limit rejected explicit %s view: %v", view, err)
		}
	}
}

func assertBoundedSummaryRejection(t *testing.T, saved *report.Report, failure *report.Error, compiled *jsonschema.Schema) {
	t.Helper()
	d := failure.Diagnostic
	if d.Recovery == nil || d.Recovery.Action != "inspect_report" || d.Recovery.Command != "report" || d.Recovery.ReportFrom != "original_argv.report_operand" || !strings.Contains(d.Message, saved.AttemptID) || !strings.Contains(d.Message, "--view diagnostics --details") {
		t.Fatal("summary rejection lost actionable caller-owned navigation")
	}
	bounded := report.NewCommandError("report", failure.Status(), []report.Diagnostic{d}, programName)
	encoded, err := report.Encode(bounded)
	var human strings.Builder
	if err != nil || len(encoded)+1 > 2048 || bounded.RenderText(&human) != nil || human.Len() > 2048 {
		t.Fatal("summary rejection itself exceeds transport")
	}
	if schemaVerdict(t, compiled, encoded) != schemaAccept {
		t.Fatal("summary rejection violates response schema")
	}
}
