// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestRecoveryVariantsRequireTheirActionData(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		recovery report.Recovery
		valid    bool
	}{
		{"root help", report.Recovery{Action: "open_help"}, true},
		{"edit location", report.Recovery{Action: fixtureEditInput, Location: &report.Location{File: argvFile}}, true},
		{"edit reference", report.Recovery{Action: fixtureEditInput, LocationFrom: "original_argv"}, true},
		{"new report", report.Recovery{Action: fixtureChooseNewReport, ReportFrom: originalReportReference}, true},
		{"inspect report", report.Recovery{Action: "inspect_report", ReportFrom: originalReportReference}, true},
		{
			"recover report",
			report.Recovery{Action: "recover_report", ReportFrom: "unused_report_target", RecoveryFrom: "publication.recovery_report"},
			true,
		},
		{"edit without location", report.Recovery{Action: fixtureEditInput}, false},
		{"inspect without report", report.Recovery{Action: "inspect_report"}, false},
		{"recover without recovery file", report.Recovery{Action: "recover_report", ReportFrom: originalReportReference}, false},
		{"unsupported action", report.Recovery{Action: "execute_shell"}, false},
	}
	for _, tc := range cases {
		builder := report.NewBuilder(testCommandCheck)
		builder.AddDiagnostic(
			0,
			report.Diagnostic{Stage: fixtureUsageStage, Code: fixtureBadFlagCode, Message: fixtureUnknownOption, Recovery: &tc.recovery},
		)

		saved := builder.Build(report.StatusInvalid)
		if err := saved.Validate(); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%t, got %v", tc.name, tc.valid, err)
		}
	}
}

func TestPublicationWriteFactsRejectContradictions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		written string
		state   report.WriteState
		valid   bool
	}{
		{string(report.ReportWritten), report.ReportWritten, true},
		{fixtureNotWritten, report.ReportFailed, true},
		{string(report.ReportWritten), report.ReportFailed, false},
		{fixtureNotWritten, report.ReportWritten, false},
	} {
		saved := mustDecode(t, richFailure)
		saved.Publication.ReportWrite = tc.written
		saved.Publication.ReportStatus = tc.state
		saved.Publication.RecoveryReport = ""

		saved.Publication.RecoveryState = ""
		if err := saved.Validate(); (err == nil) != tc.valid {
			t.Errorf("%s / %s: valid=%t, got %v", tc.written, tc.state, tc.valid, err)
		}
	}
}

func TestNoReportReceiptDoesNotInventContinuation(t *testing.T) {
	t.Parallel()

	saved := mustDecode(t, failedCheck)
	summary := saved.Summary()
	summary.BindContinuation(programName, "", originalReportReference)

	if summary.Next != nil || summary.NextOmitted || summary.NextReference != nil {
		t.Fatal("no report receipt invented report navigation")
	}
}

func TestCommandErrorHumanHelpAndOmittedHelp(t *testing.T) {
	t.Parallel()

	diagnostic := report.Diagnostic{Stage: fixtureUsageStage, Code: fixtureBadFlagCode, Message: fixtureUnknownOption}
	for _, executable := range []string{programName, strings.Repeat("x", 3000)} {
		fault := report.NewCommandError(commandReport, report.StatusInvalid, []report.Diagnostic{diagnostic}, executable)

		var text strings.Builder
		if err := fault.RenderText(&text); err != nil {
			t.Fatal(err)
		}

		if text.Len() > expectedSummaryBytes || !strings.Contains(text.String(), "help") || (fault.Next == nil) != fault.NextOmitted {
			t.Fatal("human help lost exact or omitted navigation")
		}
	}
}

func TestCommandErrorLongReportLocationReferencesOriginalOperand(t *testing.T) {
	t.Parallel()

	long := "/" + strings.Repeat("a/", 3000)
	location := &report.Location{File: long, Pointer: long}

	fault := report.NewCommandError(commandReport, report.StatusInvalid, []report.Diagnostic{{
		Stage: "shape", Code: "report_invalid_value", Message: "invalid saved-report member", Location: location,
		Recovery: &report.Recovery{Action: fixtureEditInput, Location: location},
	}}, programName)
	if fault.Diagnostics[0].Recovery.LocationFrom != fixtureQueryReportReference {
		t.Fatal("long report error pointed at unavailable command details")
	}
}
