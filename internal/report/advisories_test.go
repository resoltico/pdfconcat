// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func advisoryFixture(t *testing.T, findings []report.TextFinding) *report.Builder {
	t.Helper()
	base := mustDecode(t, completeCheck)
	builder := report.NewBuilder(testCommandCheck)
	builder.SetPhases(base.Phases)
	builder.SetCounts(report.Counts{SourcePages: new(int64(0)), GeneratedPages: new(int64(1001)), TotalPages: new(int64(1001))})
	builder.Font(base.Fonts[0])
	style := base.Styles[0]

	style.Text.Overflow = fixtureOverflowAllow
	style.Text.Findings = findings

	index := builder.Style(&style)
	builder.AddPart(report.Part{
		ID: firstItemPointer, Kind: report.PartBlank, Origin: report.Position{File: jobName}, Style: &index,
		Pages: new(int64(1000)), Range: &report.PageRange{Start: 1, End: 1000},
	})
	builder.AddPart(report.Part{
		ID: pointerItemOne, Kind: report.PartBlank, Origin: report.Position{File: jobName}, Style: &index,
		Pages: new(int64(1)), Range: &report.PageRange{Start: 1001, End: 1001},
	})

	return builder
}

func TestSharedLayoutWarningsCountRecordsAndKeepConsumerEvidence(t *testing.T) {
	t.Parallel()
	builder := advisoryFixture(t, measuredOverflow())

	saved := builder.Build(report.StatusOK)
	if saved.DiagnosticCount != 1 || saved.ErrorCount != 0 || saved.WarningCount != 1 || len(saved.Diagnostics[0].Consumers) != 2 {
		t.Fatalf("warning duplicated by counted pages or contributions: %+v", saved)
	}

	decoded := mustDecode(t, encodeReport(t, saved))
	if decoded.Diagnostics[0].Severity != report.SeverityWarning || decoded.Diagnostics[0].ConsequenceContext != "predicted" {
		t.Fatal("check warning lost its predicted context")
	}

	assertWarningSummary(t, decoded)
	assertWarningSelection(t, decoded)

	if builder.Build(report.StatusOK).WarningCount != 1 {
		t.Fatal("snapshot duplicated projected warnings")
	}
}

func TestInBoundsAllowDoesNotInventWarnings(t *testing.T) {
	t.Parallel()

	saved := advisoryFixture(t, nil).Build(report.StatusOK)
	if saved.WarningCount != 0 || len(saved.Diagnostics) != 0 {
		t.Fatal("allow alone triggered a warning")
	}
}

func TestCapturedWarningContextFollowsPublicationAndHistoricalQuery(t *testing.T) {
	t.Parallel()
	builder := advisoryFixture(t, measuredOverflow())
	builder.AddDiagnostic(0, report.Diagnostic{Stage: "publish", Code: report.CodeWriteFailed, Message: "report publication failed"})

	saved := builder.Build(report.StatusFailed)
	if saved.Diagnostics[0].Severity != report.SeverityError || saved.ErrorCount != 1 || saved.WarningCount != 1 {
		t.Fatal("fault was displaced by advisory")
	}

	if saved.Diagnostics[1].ConsequenceContext != "predicted" {
		t.Fatal("unpublished failure claimed a committed effect")
	}

	builder.SetPublication(report.Publication{Published: true, Output: "/output.pdf", ReportStatus: report.ReportNotRequested})
	builder.SetPhases(
		report.Phases{
			Instructions:       report.PhaseComplete,
			InputInspection:    report.PhaseComplete,
			Layout:             report.PhaseComplete,
			OutputVerification: report.PhaseComplete,
		},
	)
	saved = builder.Build(report.StatusFailed)
	decoded := mustDecode(t, encodeReport(t, saved))

	query := report.QueryResultOf(
		decoded,
		queryOK(t, decoded, report.Request{View: report.ViewDiagnostics}),
		programName,
		fixtureReportPath,
	)
	if query.Status != report.StatusOK || query.ErrorCount != 0 || query.SavedRun.ErrorCount != 1 || query.SelectionCounts.ErrorCount != 1 {
		t.Fatal("successful query lost historical errors or confused operation scope")
	}

	if decoded.Diagnostics[1].ConsequenceContext != fixtureContextCommitted {
		t.Fatal("published consequence remained predicted")
	}

	assertCommittedQueryText(t, query)
}

func TestSeverityAndCountInvariantsRejectContradictoryEvidence(t *testing.T) {
	t.Parallel()

	saved := advisoryFixture(t, measuredOverflow()).Build(report.StatusOK)
	for _, change := range []func(*report.Report){
		func(r *report.Report) { r.ErrorCount = 1 },
		func(r *report.Report) { r.WarningCount = 1000 },
		func(r *report.Report) { r.DiagnosticCount = 2 },
		func(r *report.Report) { r.Diagnostics[0].Severity = "notice" },
		func(r *report.Report) { r.Diagnostics[0].ConsequenceContext = fixtureContextCommitted },
		func(r *report.Report) { r.Diagnostics[0].Severity = report.SeverityError },
		func(r *report.Report) { r.Status = report.StatusFailed },
	} {
		copyReport := *saved
		copyReport.Diagnostics = append([]report.Diagnostic{}, saved.Diagnostics...)
		change(&copyReport)

		if err := copyReport.Validate(); err == nil {
			t.Fatal("contradictory severity/count/context accepted")
		}
	}
}

func TestErrorRecordsCannotCarryPredictedOrCommittedWarningContext(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, failedCheck)

	saved.Diagnostics[0].ConsequenceContext = fixtureContextCommitted
	if err := saved.Validate(); err == nil {
		t.Fatal("historical error was mislabeled as a policy consequence")
	}
}

func measuredOverflow() []report.TextFinding {
	return []report.TextFinding{{Kind: findingOutsideHorizontal, Detail: "text extends past the page", Line: -1}}
}

func assertWarningSummary(t *testing.T, decoded *report.Report) {
	t.Helper()

	summary := decoded.Summary()
	summary.BindContinuation(programName, fixtureReportPath, originalReportReference)

	if len(summary.Diagnostics) != 1 || summary.Diagnostics[0].Recovery == nil || summary.Next == nil {
		t.Fatal("default success concealed actionable warning or evidence route")
	}

	if (*summary.Next)[len(*summary.Next)-1] != report.ViewDiagnostics {
		t.Fatal("warning did not route to diagnostic evidence")
	}
}

func assertWarningSelection(t *testing.T, decoded *report.Report) {
	t.Helper()

	page := queryAs[report.PageResponse](t, decoded, report.Request{Page: new(int64(999))})
	if page.Part.WarningCount != 1 || !page.Part.Generated.Text.HasFindings {
		t.Fatal("page preview concealed findings")
	}

	response := queryOK(t, decoded, report.Request{View: report.ViewParts})

	query := report.QueryResultOf(decoded, response, programName, fixtureReportPath)
	if query.WarningCount != 0 || query.SavedRun.WarningCount != 1 || query.SelectionCounts.WarningCount != 1 ||
		query.DiagnosticsReference == nil {
		t.Fatalf("query conflated warning scope: %+v", query)
	}
}

func assertCommittedQueryText(t *testing.T, query *report.QueryResult) {
	t.Helper()

	var text strings.Builder
	if err := query.RenderText(&text); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(text.String(), "saved attempt") || !strings.Contains(text.String(), fixtureContextCommitted) {
		t.Fatal("human query lost captured consequence scope")
	}
}
