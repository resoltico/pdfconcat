// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import (
	"strconv"
	"testing"
)

func TestSummaryRetainsPrimaryAndEarliestReportSafetyWithoutDuplicates(t *testing.T) {
	t.Parallel()

	builder := NewBuilder("check")

	for index := range 8 {
		diagnostic := Diagnostic{Stage: "instructions", Code: "fault", Message: "ordinary " + strconv.Itoa(index)}
		if index == 0 {
			diagnostic.Message = "primary input fault"
		}

		if index == 5 || index == 6 {
			diagnostic.Message = "report safety " + strconv.Itoa(index)
			diagnostic.Recovery = &Recovery{Action: "choose_new_report", ReportFrom: "original_argv.--report"}
		}

		builder.AddDiagnostic(index, diagnostic)
	}

	saved := builder.Build(StatusInvalid)
	saved.Publication = Publication{
		ReportStatus: ReportFailed, ReportPath: "/r.json", ReportWrite: "not_written", ReportTargetObservation: "unknown",
	}

	if err := saved.Validate(); err != nil {
		t.Fatalf("summary fixture must be valid captured evidence: %v", err)
	}

	brief := saved.Summary()
	want := []string{"primary input fault", "report safety 5", "ordinary 1", "ordinary 2", "ordinary 3"}

	if len(brief.Diagnostics) != len(want) || len(brief.Diagnostics)+brief.DiagnosticsOmitted != 8 {
		t.Fatalf("summary diagnostic selection/count: shown=%d omitted=%d", len(brief.Diagnostics), brief.DiagnosticsOmitted)
	}

	for index, message := range want {
		if brief.Diagnostics[index].Message != message {
			t.Fatalf("summary displaced earliest actionable fault at %d: %q, want %q", index, brief.Diagnostics[index].Message, message)
		}
	}
}

func TestSummarySelectionHandlesEverySmallCountAndSafetyPosition(t *testing.T) {
	t.Parallel()

	for total := range 2*PreviewDiagnostics + 2 {
		for safety := -1; safety < total; safety++ {
			t.Run(strconv.Itoa(total)+"/"+strconv.Itoa(safety), func(t *testing.T) {
				t.Parallel()

				brief := summarySelectionFixture(total, safety).Summary()
				requireSelectionCountsAndUniqueIndices(t, brief, total)
				requireSelectionPriorities(t, brief, total, safety)
			})
		}
	}
}

func summarySelectionFixture(total, safety int) *Report {
	builder := NewBuilder("check")

	for index := range total {
		diagnostic := Diagnostic{Stage: "instructions", Code: "fault", Message: strconv.Itoa(index)}
		if index == safety {
			diagnostic.Recovery = &Recovery{Action: recoveryChooseNewReport, ReportFrom: "original_argv.--report"}
		}

		builder.AddDiagnostic(index, diagnostic)
	}

	return builder.Build(StatusInvalid)
}

func requireSelectionCountsAndUniqueIndices(t *testing.T, brief *Summary, total int) {
	t.Helper()

	if len(brief.Diagnostics) != min(total, PreviewDiagnostics) || len(brief.Diagnostics)+brief.DiagnosticsOmitted != total {
		t.Fatalf("selection lost counts: %d shown, %d omitted, %d total", len(brief.Diagnostics), brief.DiagnosticsOmitted, total)
	}

	seen := make(map[string]bool)

	for position := range brief.Diagnostics {
		diagnostic := &brief.Diagnostics[position]

		index, err := strconv.Atoi(diagnostic.Message)
		if err != nil || index < 0 || index >= total || seen[diagnostic.Message] {
			t.Fatalf("invalid or duplicated selection at %d: %q", position, diagnostic.Message)
		}

		seen[diagnostic.Message] = true
	}
}

func requireSelectionPriorities(t *testing.T, brief *Summary, total, safety int) {
	t.Helper()

	if total > 0 && brief.Diagnostics[0].Message != "0" {
		t.Fatal("selection displaced the primary diagnostic")
	}

	if safety > 0 && brief.Diagnostics[1].Message != strconv.Itoa(safety) {
		t.Fatal("selection displaced the report safety diagnostic")
	}
}
