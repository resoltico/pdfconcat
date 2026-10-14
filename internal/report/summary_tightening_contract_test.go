// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestSummaryWarningPrimaryPrecedesLaterReportSafety(t *testing.T) {
	t.Parallel()
	builder := report.NewBuilder("check")
	builder.SetPhases(report.Phases{Instructions: report.PhaseComplete, InputInspection: report.PhaseComplete, Layout: report.PhaseComplete, OutputVerification: report.PhaseNotRun})
	builder.SetCounts(report.Counts{SourcePages: new(int64), GeneratedPages: new(int64), TotalPages: new(int64)})
	for index, message := range []string{"primary warning", "ordinary warning", "later report safety"} {
		diagnostic := report.Diagnostic{Severity: report.SeverityWarning, Stage: report.StageWrite, Code: "advisory", Message: message}
		if index == 2 {
			diagnostic.Recovery = &report.Recovery{Action: "choose_new_report", ReportFrom: originalReportReference}
		}
		builder.AddDiagnostic(index, diagnostic)
	}
	saved := builder.Build(report.StatusOK)
	if err := saved.Validate(); err != nil {
		t.Fatalf("invalid captured warning fixture: %v", err)
	}
	brief := saved.Summary()
	for index, message := range []string{"primary warning", "later report safety", "ordinary warning"} {
		if brief.Diagnostics[index].Message != message {
			t.Fatalf("warning priority %d: %q, want %q", index, brief.Diagnostics[index].Message, message)
		}
	}
}

func TestQuerySummaryPreservesMessagesAfterTighteningPublicationPreviews(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		name, message         string
		tokenLength, retained int
	}{
		{"first fitting message preview", "m", 44, 128},
		{"second fitting message preview", "m", 64, 64},
		{"escaped message preview", "\x01", 64, 10},
		{"UTF-8 message preview", "😀", 44, 32},
	} {
		t.Run(item.name, func(t *testing.T) {
			saved := summaryFailureWithProtectedRecovery(t)
			for index := range saved.Diagnostics {
				saved.Diagnostics[index].Stage = report.Stage(strings.Repeat("s", item.tokenLength))
				saved.Diagnostics[index].Code = report.Code(strings.Repeat("c", item.tokenLength))
				saved.Diagnostics[index].Message = strings.Repeat(item.message, 256)
			}
			saved = mustDecode(t, encodeReport(t, saved))
			query := report.QueryResultOf(saved, queryOK(t, saved, report.Request{}), programName, fixtureReportPath)
			brief, found := report.ContentOf[report.Summary](query.Result)
			if !found || len(brief.Diagnostics) != 2 || brief.DiagnosticsOmitted != 0 {
				t.Fatal("nested summary lost primary or protected recovery")
			}
			for index := range brief.Diagnostics {
				if brief.Diagnostics[index].Message != strings.Repeat(item.message, item.retained) || !brief.Diagnostics[index].MessageTruncated {
					t.Fatalf("message %d lost detail after fitting or failed to carry a cut flag: %q", index, brief.Diagnostics[index].Message)
				}
				if saved.Diagnostics[index].Message != strings.Repeat(item.message, 256) || brief.Diagnostics[index].Code != saved.Diagnostics[index].Code {
					t.Fatal("preview changed captured message or required diagnostic code")
				}
			}
			if brief.Diagnostics[1].Recovery == nil || brief.Diagnostics[1].Recovery.Action != "choose_new_report" {
				t.Fatal("preview erased required report recovery")
			}
			if brief.Publication.Output != strings.Repeat("o", 12) || brief.Publication.ReportPath != strings.Repeat("r", 12) {
				t.Fatal("publication previews lost bytes beyond the three required tightening stages")
			}
			payload, err := report.Encode(query)
			var human strings.Builder
			if err != nil || len(payload)+1 > 2048 || query.RenderText(&human) != nil || human.Len() > 2048 {
				t.Fatalf("nested summary exceeds transport: JSON%d human%d error%v", len(payload)+1, human.Len(), err)
			}
		})
	}
}

func TestSummaryRetainsFittingContinuationAfterForeignPreviewCut(t *testing.T) {
	t.Parallel()
	saved := summaryFailureWithProtectedRecovery(t)
	saved.Diagnostics = saved.Diagnostics[:1]
	saved.Diagnostics[0] = report.Diagnostic{Severity: report.SeverityError, Stage: "write", Code: "write_failed", Message: "primary failure", Cause: "x"}
	saved.FinalizeDiagnostics()
	initial, err := report.Encode(saved.Summary())
	if err != nil {
		t.Fatal(err)
	}
	saved.Diagnostics[0].Cause = strings.Repeat("x", 2040-len(initial))
	saved = mustDecode(t, encodeReport(t, saved))
	brief := saved.Summary()
	initial, err = report.Encode(brief)
	if err != nil || len(initial)+1 != 2040 || brief.Diagnostics[0].Cause != saved.Diagnostics[0].Cause {
		t.Fatalf("independently sized near-limit fixture did not retain its fitting cause: %d/%v", len(initial)+1, err)
	}
	brief.BindContinuation(programName, fixtureReportPath, originalReportReference)
	if brief.Next == nil || brief.NextOmitted || brief.Diagnostics[0].Cause == saved.Diagnostics[0].Cause {
		t.Fatal("a fitting continuation was lost after optional foreign detail was cut")
	}
	if got := nextArguments(brief.Next); len(got) != 7 || got[0] != programName || got[2] != fixtureReportPath || got[4] != saved.AttemptID {
		t.Fatalf("caller-owned continuation changed: %v", got)
	}
	payload, err := report.Encode(brief)
	if err != nil || len(payload)+1 > 2048 {
		t.Fatalf("continuation transport: %d/%v", len(payload)+1, err)
	}
}

func TestQuerySummaryContainerCutLabelsPreserveVisibleRecords(t *testing.T) {
	t.Parallel()
	saved := summaryFailureWithProtectedRecovery(t)
	for i := range saved.Diagnostics {
		saved.Diagnostics[i].Stage = report.Stage(strings.Repeat("s", 64))
		saved.Diagnostics[i].Code = report.Code(strings.Repeat("c", 64))
	}
	saved.Diagnostics = append(saved.Diagnostics, report.Diagnostic{Severity: report.SeverityWarning, Stage: "layout", Code: "advisory", Message: "additional advisory"})
	saved.FinalizeDiagnostics()
	saved = mustDecode(t, encodeReport(t, saved))
	query := report.QueryResultOf(saved, queryOK(t, saved, report.Request{}), programName, fixtureReportPath)
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	brief, _ := report.ContentOf[report.Summary](query.Result)
	if len(brief.Diagnostics) != 2 || brief.DiagnosticsOmitted != 1 {
		t.Fatal("container cut label erased a visible safety record")
	}
	want := []string{"diagnostics/0", "diagnostics/1", "publication"}
	if !reflect.DeepEqual(brief.TruncatedFields, want) {
		t.Fatalf("cut labels %q, want visible container scopes%q", brief.TruncatedFields, want)
	}
	for i := range brief.Diagnostics {
		if brief.Diagnostics[i].Code != saved.Diagnostics[i].Code || saved.Diagnostics[i].Message != strings.Repeat("m", 256) {
			t.Fatal("container labels changed code or complete evidence")
		}
	}
}
