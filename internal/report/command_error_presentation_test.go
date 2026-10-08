// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

const (
	commandHelpOption       = "--help"
	commandField            = "command"
	recoveryReportReference = "publication.recovery_report"
	commandWireFormat       = `{"format_version":2,"kind":"error","status":"invalid","command":"report",` +
		`"next":["%s","report","--help"],"diagnostics":[]}`
)

func TestCommandErrorHelpUsesCommandAndRequiresExecutableAuthority(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		command, executable string
		next                []string
		omitted             bool
	}{
		{"check", programName, []string{programName, "check", commandHelpOption}, false},
		{"", programName, []string{programName, commandHelpOption}, false},
		{commandReport, "", nil, true},
	} {
		fault := report.NewCommandError(test.command, report.StatusInvalid, nil, test.executable)
		if (fault.Next == nil) != test.omitted || fault.NextOmitted != test.omitted {
			t.Fatalf("help authority: %+v", fault)
		}

		if !test.omitted && !slices.Equal(*fault.Next, test.next) {
			t.Fatalf("wrong command help argv: %v", fault.Next)
		}

		if test.omitted && fault.ExecutableFrom != "invoking_executable" {
			t.Fatalf("missing caller-owned executable reference: %+v", fault)
		}
	}
}

func TestCommandErrorHumanCuesDescribeActualTruncationAndOmission(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, truncatedText, omittedText string
		fields                           []string
		omitted                          int
	}{
		{"complete", "", "", nil, 0},
		{"truncated", "truncated previews: [command]\n", "", []string{commandField}, 0},
		{"omitted", "", "diagnostics omitted: 3\n", nil, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fault := &report.CommandError{Status: report.StatusInvalid, TruncatedFields: test.fields, DiagnosticsOmitted: test.omitted}

			var text strings.Builder
			if err := fault.RenderText(&text); err != nil {
				t.Fatal(err)
			}

			assertCommandLossCue(t, text.String(), "truncated previews:", test.truncatedText)
			assertCommandLossCue(t, text.String(), "diagnostics omitted:", test.omittedText)
		})
	}
}

func assertCommandLossCue(t *testing.T, rendered, prefix, expected string) {
	t.Helper()

	if strings.Contains(rendered, prefix) != (expected != "") {
		t.Fatalf("false human loss cue: %q", rendered)
	}

	if expected != "" && !strings.Contains(rendered, expected) {
		t.Fatalf("wrong human loss cue: %q", rendered)
	}
}

func TestCommandErrorJSONLineBoundaryPreservesExactlyFittingHelp(t *testing.T) {
	t.Parallel()
	// An independent literal public wire shape includes the newline in its 2048-byte budget.
	base := fmt.Sprintf(commandWireFormat, "")

	padding := 2048 - 1 - len(base)
	for _, delta := range []int{-1, 0, 1} {
		assertCommandLineBoundary(t, padding, delta)
	}
}

func assertCommandLineBoundary(t *testing.T, padding, delta int) {
	t.Helper()

	executable := strings.Repeat("x", padding+delta)

	reference := fmt.Sprintf(commandWireFormat, executable)
	if !json.Valid([]byte(reference)) || len(reference)+1 != 2048+delta {
		t.Fatalf("wire boundary calibration: %d", len(reference)+1)
	}

	fault := report.NewCommandError(commandReport, report.StatusInvalid, nil, executable)
	if delta <= 0 {
		args := []string{executable, commandReport, commandHelpOption}
		if fault.Next == nil || fault.NextOmitted || !slices.Equal(*fault.Next, args) {
			t.Fatalf("fitting help unnecessarily omitted at %d", len(reference)+1)
		}
	} else if fault.Next != nil || !fault.NextOmitted {
		t.Fatalf("oversized help exposed at %d", len(reference)+1)
	}

	encoded, err := report.Encode(fault)
	if err != nil || len(encoded)+1 > 2048 {
		t.Fatalf("command error exceeds public line budget: %d %v", len(encoded)+1, err)
	}
}

func TestCommandErrorDiagnosticBoundingKeepsPrimaryAndExactLossCount(t *testing.T) {
	t.Parallel()

	diagnostics := make([]report.Diagnostic, 5)
	for index := range diagnostics {
		diagnostics[index] = report.Diagnostic{
			Stage:    fixtureUsageStage,
			Code:     fixtureBadFlagCode,
			Message:  strings.Repeat("message ", 40),
			Cause:    strings.Repeat("cause ", 40),
			Path:     strings.Repeat("path ", 40),
			Location: &report.Location{File: strings.Repeat("file ", 40), Pointer: strings.Repeat("/node", 40), ArgvIndex: new(index)},
		}
	}

	fault := report.NewCommandError(commandReport, report.StatusInvalid, diagnostics, programName)

	encoded, err := report.Encode(fault)
	if err != nil || len(encoded)+1 > 2048 {
		t.Fatalf("rich diagnostic error exceeds budget: %d %v", len(encoded)+1, err)
	}

	if len(fault.Diagnostics) != 2 || fault.DiagnosticsOmitted != 5-len(fault.Diagnostics) {
		t.Fatalf("visible diagnostic accounting: shown%d omitted%d", len(fault.Diagnostics), fault.DiagnosticsOmitted)
	}

	for index := range fault.Diagnostics {
		location := fault.Diagnostics[index].Location
		if location == nil || location.ArgvIndex == nil || *location.ArgvIndex != index {
			t.Fatalf("primary/order identity lost at%d: %+v", index, location)
		}
	}
}

func TestCommandErrorTextRemainsShorterThanHostileTypedJSON(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "Ā Ελληνικά Кириллица🙂", "\"\\<>&\n\t", string([]byte{0xff}), strings.Repeat("path/", 300)} {
		for _, executable := range []string{programName, "", strings.Repeat("x", 2200)} {
			diagnostics := hostileCommandDiagnostics(value)

			fault := report.NewCommandError(commandReport, report.StatusInvalid, diagnostics, executable)

			encoded, err := report.Encode(fault)
			if err != nil {
				t.Fatal(err)
			}

			var text strings.Builder
			if err = fault.RenderText(&text); err != nil {
				t.Fatal(err)
			}

			if text.Len() >= len(encoded)+1 || text.Len() > 2048 {
				t.Fatalf("typed human/JSON budget dominance lost: text%d JSONline%d", text.Len(), len(encoded)+1)
			}
		}
	}
}

func hostileCommandDiagnostics(value string) []report.Diagnostic {
	diagnostics := make([]report.Diagnostic, 5)
	for index := range diagnostics {
		location := &report.Location{
			File:      value,
			Pointer:   value,
			Line:      2147483647,
			Column:    2147483647,
			Offset:    new(int64(9223372036854775807)),
			ArgvIndex: new(index),
		}
		diagnostics[index] = report.Diagnostic{
			Stage:    fixtureUsageStage,
			Code:     fixtureBadFlagCode,
			Message:  value,
			Cause:    value,
			Path:     value,
			Location: location,
			Recovery: &report.Recovery{Action: fixtureEditInput, Location: location, Replacement: fixtureBlankOption},
		}
	}

	return diagnostics
}

func TestCommandErrorPrimarySchemaBoundsFitJSONLine(t *testing.T) {
	t.Parallel()

	location := &report.Location{
		File:    strings.Repeat("\"", 200),
		Pointer: strings.Repeat("\\", 200),
		Line:    9223372036854775807,
		Column:  9223372036854775807,
		Offset:  new(int64(9223372036854775807)),
	}
	recoveryLocation := &report.Location{
		File:    strings.Repeat("f", 96),
		Pointer: strings.Repeat("p", 96),
		Line:    9223372036854775807,
		Column:  9223372036854775807,
		Offset:  new(int64(9223372036854775807)),
	}
	diagnostic := report.Diagnostic{
		Stage:    report.Stage(strings.Repeat("s", 64)),
		Code:     report.Code(strings.Repeat("c", 64)),
		Message:  strings.Repeat("Ā", 256),
		Path:     strings.Repeat("\x01", 200),
		Cause:    strings.Repeat("\"", 200),
		Location: location,
		Recovery: &report.Recovery{
			Action:       fixtureChooseNewReport,
			Command:      "version",
			Replacement:  "--" + strings.Repeat("x", 63),
			Location:     recoveryLocation,
			LocationFrom: "complete_report.diagnostics/9999999/recovery/location",
			ReportFrom:   fixtureQueryReportReference,
			RecoveryFrom: recoveryReportReference,
		},
	}

	diagnostics := make([]report.Diagnostic, 5)
	for index := range diagnostics {
		diagnostics[index] = diagnostic
	}

	fault := report.NewCommandError(strings.Repeat("q", 200), report.StatusInterrupted, diagnostics, strings.Repeat("x", 3000))

	encoded, err := report.Encode(fault)
	if err != nil || len(encoded)+1 > 2048 || len(fault.Diagnostics) != 1 || fault.DiagnosticsOmitted != 4 {
		t.Fatalf(
			"supported primary exceeds line bound: bytes%d shown%d omitted%d %v",
			len(encoded)+1,
			len(fault.Diagnostics),
			fault.DiagnosticsOmitted,
			err,
		)
	}

	schema := compileSchema(t, report.ResponseSchema(), responseSchemaURL)
	if schemaVerdict(t, schema, encoded) != schemaAccept {
		t.Fatalf("primary premise is not a supported schema shape: %s", encoded)
	}
}
