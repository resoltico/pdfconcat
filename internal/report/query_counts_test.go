// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestQueryScopesAcceptErrorWarningAndMixedHistory(t *testing.T) {
	t.Parallel()
	warning := advisoryFixture(t, measuredOverflow()).Build(report.StatusOK)
	builder := advisoryFixture(t, measuredOverflow())
	builder.AddDiagnostic(0, report.Diagnostic{Stage: report.StageWrite, Code: report.CodeWriteFailed, Message: "write failed"})

	for _, test := range []struct {
		saved            *report.Report
		name             string
		errors, warnings int
	}{
		{saved: mustDecode(t, failedCheck), name: "errors", errors: 2},
		{saved: warning, name: "warnings", warnings: 1},
		{saved: builder.Build(report.StatusFailed), name: "mixed", errors: 1, warnings: 1},
	} {
		query := report.QueryResultOf(
			test.saved,
			queryOK(t, test.saved, report.Request{View: report.ViewDiagnostics}),
			programName,
			fixtureReportPath,
		)
		if err := query.Validate(); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}

		if query.ErrorCount != 0 || query.WarningCount != 0 || query.SavedRun.ErrorCount != test.errors ||
			query.SavedRun.WarningCount != test.warnings {
			t.Fatalf("%s: operation consumed historical diagnostic severity", test.name)
		}

		if query.SelectionCounts.ErrorCount != test.errors || query.SelectionCounts.WarningCount != test.warnings {
			t.Fatalf("%s: selected counts lost historical records", test.name)
		}

		payload, err := report.Encode(query)
		if err != nil || schemaVerdict(t, compileSchema(t, report.ResponseSchema(), responseSchemaURL), payload) != schemaAccept {
			t.Fatalf("%s: schema refused historical records: %v", test.name, err)
		}
	}
}

func TestQueryScopeInvariantsRejectMismatchedCounts(t *testing.T) {
	t.Parallel()

	saved := advisoryFixture(t, measuredOverflow()).Build(report.StatusOK)
	for _, change := range []func(*report.QueryResult){
		func(q *report.QueryResult) { q.ErrorCount = 1 },
		func(q *report.QueryResult) { q.Status = report.StatusFailed },
		func(q *report.QueryResult) { q.SavedRun.WarningCount = 1001 },
		func(q *report.QueryResult) { q.SavedRun.Status = report.StatusFailed },
		func(q *report.QueryResult) { q.SavedRun.Status = "unknown" },
		func(q *report.QueryResult) { q.SelectionCounts.ErrorCount = 1 },
		func(q *report.QueryResult) { q.SelectionCounts = report.DiagnosticCounts{} },
		func(q *report.QueryResult) {
			q.SelectionCounts = report.DiagnosticCounts{DiagnosticCount: 2, WarningCount: 2}
		},
	} {
		query := report.QueryResultOf(
			saved,
			queryOK(t, saved, report.Request{View: report.ViewDiagnostics}),
			programName,
			fixtureReportPath,
		)
		change(query)

		found, ok := report.AsError(query.Validate())
		if !ok || found.Status() != report.StatusFailed {
			t.Fatal("inconsistent producer scope was accepted or blamed on instructions")
		}
	}
}

func TestSelectedQueryRecordsCannotDisagreeWithTheirScopedCounts(t *testing.T) {
	t.Parallel()

	saved := advisoryFixture(t, measuredOverflow()).Build(report.StatusOK)
	for _, request := range []report.Request{
		{}, {Part: new(firstItemPointer)}, {Page: new(int64(1))}, {View: report.ViewDiagnostics},
	} {
		query := report.QueryResultOf(saved, queryOK(t, saved, request), programName, fixtureReportPath)
		if err := query.Validate(); err != nil {
			t.Fatal(err)
		}

		query.SelectionCounts = report.DiagnosticCounts{}
		if err := query.Validate(); err == nil {
			t.Fatal("selected record warning was accepted with zero scoped count")
		}
	}
}

func TestSelectedDiagnosticSeveritiesMustBeKnown(t *testing.T) {
	t.Parallel()
	saved := advisoryFixture(t, measuredOverflow()).Build(report.StatusOK)
	response := queryOK(t, saved, report.Request{View: report.ViewDiagnostics})
	query := report.QueryResultOf(saved, response, programName, fixtureReportPath)

	view, ok := report.ContentOf[report.ViewResponse[report.DiagnosticView]](response)
	if !ok || len(view.Records) != 1 {
		t.Fatal("missing actual selected diagnostic record")
	}

	view.Records[0].Severity = "notice"

	if err := query.Validate(); err == nil {
		t.Fatal("unknown selected diagnostic severity accepted")
	}
}

func TestACompleteReportIsNotAnUncheckedQuerySelection(t *testing.T) {
	t.Parallel()
	saved := advisoryFixture(t, measuredOverflow()).Build(report.StatusOK)

	query := report.QueryResultOf(saved, report.NewResponse(saved), programName, fixtureReportPath)
	if err := query.Validate(); err == nil {
		t.Fatal("unsupported complete report body was accepted as a finite query selection")
	}
}

func TestSummarySelectsErrorsBeforeWarningsInSavedOrder(t *testing.T) {
	t.Parallel()
	builder := advisoryFixture(t, measuredOverflow())
	builder.AddDiagnostic(0, report.Diagnostic{Stage: report.StageWrite, Code: report.CodeWriteFailed, Message: "write failed"})
	saved := builder.Build(report.StatusFailed)

	saved.Diagnostics[0], saved.Diagnostics[1] = saved.Diagnostics[1], saved.Diagnostics[0]
	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	summary := saved.Summary()
	if summary.Diagnostics[0].Severity != report.SeverityError || saved.Diagnostics[0].Severity != report.SeverityWarning {
		t.Fatal("summary displaced a historical error or changed stored evidence order")
	}
}
