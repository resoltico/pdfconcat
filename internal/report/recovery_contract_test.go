// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestDiagnosticSnapshotOwnsRecoveryAndLocation(t *testing.T) {
	t.Parallel()

	index := 3
	location := &report.Location{File: argvFile, ArgvIndex: &index}
	recovery := &report.Recovery{Action: fixtureEditInput, Location: location, Replacement: fixtureBlankOption}
	builder := report.NewBuilder(testCommandCheck)
	builder.AddDiagnostic(
		0,
		report.Diagnostic{
			Stage:    fixtureUsageStage,
			Code:     fixtureBadFlagCode,
			Message:  fixtureUnknownOption,
			Cause:    "original error",
			Location: location,
			Recovery: recovery,
		},
	)
	location.File = fixtureMutation
	index = 9
	recovery.Replacement = fixtureMutation

	first := builder.Build(report.StatusInvalid)
	if first.Diagnostics[0].Location.File != argvFile || *first.Diagnostics[0].Recovery.Location.ArgvIndex != 3 ||
		first.Diagnostics[0].Recovery.Replacement != fixtureBlankOption {
		t.Fatal("caller mutated captured diagnostic")
	}

	first.Diagnostics[0].Recovery.Location.File = fixtureMutationAgain
	first.Diagnostics[0].Recovery.Replacement = fixtureMutationAgain

	second := builder.Build(report.StatusInvalid)
	if second.Diagnostics[0].Recovery.Location.File != argvFile || second.Diagnostics[0].Recovery.Replacement != fixtureBlankOption {
		t.Fatal("report mutated builder snapshot")
	}

	brief := second.Summary()

	brief.Diagnostics[0].Recovery.Location.File = "summary mutation"
	if second.Diagnostics[0].Recovery.Location.File != argvFile {
		t.Fatal("summary mutated complete evidence")
	}
}

func TestNewContractStructuralControlsAgreeWithSchema(t *testing.T) {
	t.Parallel()

	schema := compileSchema(t, report.Schema(), schemaURL)

	cases := []struct{ name, old, replacement string }{
		{"missing attempt", `"attempt_id":"AAAAAAAAAAAAAAAAAAAAAAAAAA",`, ""},
		{"non-base32 attempt", fixtureAttemptID, strings.Repeat("a", 26)},
		{"unicode attempt", fixtureAttemptID, strings.Repeat("ā", 26)},
		{"invalid write state", `"report_status":"written"`, `"report_status":"written","report_write":"unchanged"`},
		{
			"invalid target state", `"report_status":"written"`,
			`"report_status":"written",` + `"report_target_observation":"unchanged"`,
		},
		{
			"missing recovery command",
			memberEmptyDiagnostics,
			`"diagnostics":[{"stage":"usage","code":"bad_flag","message":"fault","recovery":{"action":"open_help"}}]`,
		},
		{
			"empty required reference",
			memberEmptyDiagnostics,
			`"diagnostics":[{"stage":"usage","code":"bad_flag","message":"fault",` +
				`"recovery":{"action":"choose_new_report","command":"","report_from":""}}]`,
		},
	}
	for _, tc := range cases {
		document := strings.Replace(completeCheck, tc.old, tc.replacement, 1)
		if _, err := decodeText(t, document); err == nil {
			t.Errorf("%s: decoder accepted invalid evidence", tc.name)
		}

		if schemaVerdict(t, schema, []byte(document)) != schemaReject {
			t.Errorf("%s: schema accepted invalid evidence", tc.name)
		}
	}

	document := strings.Replace(
		failedCheck,
		memberCannotRead,
		`"message":"cannot read","cause":"permission denied","recovery":{"action":"open_help","command":"report"}`,
		1,
	)

	saved := mustDecode(t, document)
	if saved.Diagnostics[0].Cause != "permission denied" || schemaVerdict(t, schema, []byte(document)) != schemaAccept {
		t.Fatal("valid recovery/cause evidence rejected")
	}
}

func TestCommandErrorsRetainBoundedPreviewMarkers(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("ā\\\"", 3000)

	diagnostics := make([]report.Diagnostic, 8)
	for index := range diagnostics {
		diagnostics[index] = report.Diagnostic{
			Stage:    fixtureUsageStage,
			Code:     fixtureBadFlagCode,
			Message:  "unknown option; use command help",
			Cause:    long,
			Path:     long,
			Location: &report.Location{File: long, ArgvIndex: new(index)},
			Recovery: &report.Recovery{
				Action:      "edit_input",
				Location:    &report.Location{File: long, ArgvIndex: new(index)},
				Replacement: fixtureBlankOption,
			},
		}
	}

	fault := report.NewCommandError(testCommandCheck, report.StatusInvalid, diagnostics, long)

	encoded, err := report.Encode(fault)
	if err != nil || len(encoded)+1 > expectedSummaryBytes || len(fault.TruncatedFields) == 0 || fault.DiagnosticsOmitted == 0 {
		t.Fatalf("command preview bytes=%d, omitted=%d: %v", len(encoded)+1, fault.DiagnosticsOmitted, err)
	}

	if fault.Diagnostics[0].Recovery.LocationFrom != "original_argv" || !strings.Contains(string(encoded), "unknown option") {
		t.Fatal("command cause or caller-owned recovery reference lost")
	}

	schema := compileSchema(
		t,
		report.ResponseSchema(),
		responseSchemaURL,
	)
	if schemaVerdict(t, schema, encoded) != schemaAccept {
		t.Fatalf("invalid bounded command response: %s", encoded)
	}
}

func TestSummaryKeepsPrimaryAndReportSafetyFault(t *testing.T) {
	t.Parallel()

	builder := report.NewBuilder(testCommandCheck)
	long := strings.Repeat("ā\\\"", 3000)
	builder.AddDiagnostic(
		0,
		report.Diagnostic{
			Stage: "syntax", Code: "invalid_json", Message: "invalid JSON; correct the input", Path: long,
			Location: &report.Location{File: long, Pointer: long},
			Recovery: &report.Recovery{Action: fixtureEditInput, Location: &report.Location{File: long, Pointer: long}},
		},
	)

	for index := 1; index < 8; index++ {
		builder.AddDiagnostic(index, report.Diagnostic{Stage: "input", Code: "other_fault", Message: long, Path: long})
	}

	builder.AddDiagnostic(9, report.Diagnostic{
		Stage: "publish",
		Code:  "report_write_failed",
		Cause: long,
		Message: "Before every input is known, --overwrite cannot replace a report. " +
			"Choose an unused target; an existing report may be historical.",
		Recovery: &report.Recovery{Action: fixtureChooseNewReport, ReportFrom: originalReportReference},
	})
	saved := builder.Build(report.StatusInvalid)
	saved.Publication = report.Publication{
		ReportStatus:            report.ReportFailed,
		ReportPath:              long,
		ReportWrite:             fixtureNotWritten,
		ReportTargetObservation: unknownMetadataValue,
	}
	brief := saved.Summary()
	brief.BindContinuation(strings.Repeat("x", 3000), long, originalReportReference)

	payload, err := report.Encode(brief)
	if err != nil || len(payload)+1 > expectedSummaryBytes {
		t.Fatalf("summary bytes=%d: %v", len(payload)+1, err)
	}

	if len(brief.Diagnostics) < 2 || brief.Diagnostics[0].Code != "invalid_json" || brief.Diagnostics[1].Code != "report_write_failed" ||
		brief.Diagnostics[1].Recovery.Action != "choose_new_report" {
		t.Fatal("primary or report safety action omitted")
	}

	if saved.Diagnostics[8].Cause != long {
		t.Fatal("preview mutated full foreign cause")
	}
}

func TestConcreteContinuationsRejectLossyUTF8Paths(t *testing.T) {
	t.Parallel()

	invalid := string([]byte{0xff})

	saved := mustDecode(t, completeCheck)
	for _, authority := range [][2]string{{invalid, fixtureReportPath}, {programName, invalid}} {
		summary := saved.Summary()
		summary.BindContinuation(authority[0], authority[1], originalReportReference)

		if summary.Next != nil || !summary.NextOmitted || summary.NextReference == nil {
			t.Fatal("invalid UTF-8 exposed as executable continuation")
		}
	}

	fault := report.NewCommandError(commandReport, report.StatusInvalid, []report.Diagnostic{{
		Stage: fixtureUsageStage, Code: fixtureBadFlagCode, Message: fixtureUnknownOption,
	}}, invalid)
	if fault.Next != nil || !fault.NextOmitted || fault.ExecutableFrom != "invoking_executable" {
		t.Fatal("invalid UTF-8 exposed as executable help")
	}

	valid := saved.Summary()
	valid.BindContinuation(programName, fixtureReportPath, originalReportReference)

	if valid.Next == nil || valid.NextOmitted {
		t.Fatal("valid authority unnecessarily omitted")
	}
}

func TestUntrustedRecoveryMetadataCannotBypassSummaryBounds(t *testing.T) {
	t.Parallel()

	schema := compileSchema(t, report.Schema(), schemaURL)

	long := strings.Repeat("x", 10000)
	for _, member := range []string{"command", "replacement", "report_from", "recovery_from", "location_from"} {
		recovery := `"recovery":{"action":"open_help","command":"report","` + member + `":"` + long + `"}`
		if member == "command" {
			recovery = `"recovery":{"action":"open_help","command":"` + long + `"}`
		}

		document := strings.Replace(failedCheck, memberCannotRead, `"message":"cannot read",`+recovery, 1)
		if _, err := decodeText(t, document); err == nil {
			t.Fatalf("oversized %s accepted", member)
		}

		if schemaVerdict(t, schema, []byte(document)) != schemaReject {
			t.Fatalf("schema accepted oversized %s", member)
		}
	}
}

func TestCommandErrorBoundsAnOverlongForeignCommand(t *testing.T) {
	t.Parallel()

	fault := report.NewCommandError(strings.Repeat("x", 10000), report.StatusInvalid, []report.Diagnostic{{
		Stage: fixtureUsageStage, Code: fixtureBadFlagCode, Message: fixtureUnknownOption,
	}}, programName)

	payload, err := report.Encode(fault)
	if err != nil || len(payload) >= expectedSummaryBytes || len(fault.TruncatedFields) == 0 || !fault.NextOmitted {
		t.Fatalf("foreign command preview bytes=%d: %v", len(payload)+1, err)
	}

	if err = fault.RenderText(failingWriter{}); err == nil {
		t.Fatal("lost command-render write failure")
	}
}
