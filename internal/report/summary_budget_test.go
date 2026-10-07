// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestSummaryBoundsEveryVariablePreviewAndPreservesDetail(t *testing.T) {
	t.Parallel()

	inputs := []string{
		strings.Repeat("x", 1_000_000),
		strings.Repeat("ā😀", 10000),
		strings.Repeat("\x00\"\\", 10000),
		strings.Repeat("<>&\u2028\u2029", 10000),
	}
	for _, text := range inputs {
		builder := report.NewBuilder(testCommandBuild)
		for i := range 5 {
			builder.AddDiagnostic(
				i,
				report.Diagnostic{
					Stage:    "input",
					Code:     "bad_path",
					Path:     text,
					Message:  text,
					Location: &report.Location{File: text, Pointer: text},
				},
			)
		}

		builder.SetPublication(
			report.Publication{Output: text, ReportPath: text, RecoveryReport: text, ReportStatus: report.ReportFailed, Published: true},
		)
		full := builder.Build(report.StatusFailed)
		brief := full.Summary()

		encoded, err := report.Encode(brief)
		if err != nil || len(encoded)+1 > expectedSummaryBytes {
			t.Fatalf("summary bytes=%d: %v", len(encoded)+1, err)
		}

		if !brief.Publication.Published || brief.DiagnosticCount != 5 || brief.DiagnosticsOmitted+len(brief.Diagnostics) != 5 ||
			len(brief.TruncatedFields) == 0 {
			t.Fatalf("summary state: %+v", brief)
		}

		checkUnchangedDetail(t, full, text)

		if len(brief.Next) > 0 {
			t.Fatal("truncated path exposed as exact next argv")
		}
	}
}

func checkUnchangedDetail(t *testing.T, full *report.Report, text string) {
	t.Helper()

	if full.Publication.Output != text || full.Diagnostics[0].Path != text || full.Diagnostics[0].Location.File != text {
		t.Fatal("summary mutated complete report")
	}
}

func TestSummaryKeepsLosslessRecoveryReferenceAndOmitsLongNextCommand(t *testing.T) {
	t.Parallel()

	directory := "/" + strings.Repeat("a/", 400)
	failed := mustDecode(t, richFailure)
	failed.Publication.Output = directory + "output.pdf"
	failed.Publication.ReportPath = directory + "report.json"
	failed.Publication.RecoveryReport = directory + ".pdfconcat-report-123"

	brief := failed.Summary()
	if brief.RecoveryBasename != ".pdfconcat-report-123" || brief.RecoveryDirectoryFrom != "original_report_argument" ||
		brief.Publication.RecoveryReport != "" {
		t.Fatalf("lossless recovery: %+v", brief)
	}

	success := mustDecode(t, completeCheck)
	success.Publication.Output = directory + "output.pdf"
	success.Publication.ReportPath = directory + "report.json"

	brief = success.Summary()
	if len(brief.Next) != 0 || !brief.NextOmitted {
		t.Fatalf("long argv should be omitted: %+v", brief)
	}

	for _, summary := range []*report.Summary{brief, failed.Summary()} {
		encoded, err := report.Encode(summary)
		if err != nil || len(encoded)+1 > expectedSummaryBytes {
			t.Fatalf("summary: bytes %d %v", len(encoded)+1, err)
		}
	}
}

func TestSummaryHumanRecoveryAndTruncationReferencesFitBudget(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, richFailure)
	directory := "/" + strings.Repeat("a/", 400)
	saved.Publication.Output = directory + "output.pdf"
	saved.Publication.ReportPath = directory + "report.json"
	saved.Publication.RecoveryReport = directory + ".pdfconcat-report-123"

	var output strings.Builder
	if err := saved.Summary().RenderText(&output); err != nil {
		t.Fatal(err)
	}

	if output.Len() > expectedSummaryBytes || !strings.Contains(output.String(), "original_report_argument") ||
		!strings.Contains(output.String(), "truncated previews") {
		t.Fatalf("human recovery: %s", output.String())
	}

	saved.Publication.ReportStatus = report.ReportWritten
	saved.Publication.RecoveryReport = ""
	saved.Publication.RecoveryState = ""

	output.Reset()

	if err := saved.Summary().RenderText(&output); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(output.String(), "next command omitted") {
		t.Fatal("missing safe continuation hint")
	}
}
