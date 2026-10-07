// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"encoding/json"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

// optionalCase is a value and whether the one optional member it may carry must be in its JSON object.
type optionalCase struct {
	value   any
	name    string
	member  string
	present bool
}

const (
	// Names of the optional members of the report's JSON objects.
	optionalOffset    = "offset"
	optionalLine      = "line"
	optionalColumn    = "column"
	optionalPointer   = "pointer"
	optionalArgvIndex = "argv_index"
	optionalSource    = "source"
	optionalStyle     = "style"
	optionalDigest    = "digest"
	optionalText      = "text"
	optionalPath      = "path"
	optionalGenerated = "generated"
	optionalOversized = "oversized_record"
	optionalOmitted   = "diagnostics_omitted"
	optionalNext      = "next"
)

// hasMember reports whether the JSON object a value encodes to has the member, read back with the standard
// library so that the writer's own member list is not the oracle.
func hasMember(t *testing.T, value any, member string) bool {
	t.Helper()

	data, err := report.Encode(value)
	if err != nil {
		t.Fatal(err)
	}

	var object map[string]json.RawMessage

	err = json.Unmarshal(data, &object)
	if err != nil {
		t.Fatalf("%s is not an object: %v", data, err)
	}

	_, found := object[member]

	return found
}

func locationCases() []optionalCase {
	return []optionalCase{
		{report.Position{File: "f"}, "position without offset", optionalOffset, false},
		{report.Position{File: "f", Offset: new(int64(0))}, "a zero offset is still written", optionalOffset, true},
		{report.Position{File: "f"}, "position without line", optionalLine, false},
		{report.Position{File: "f", Line: 1}, "position with line", optionalLine, true},
		{report.Position{File: "f"}, "position without column", optionalColumn, false},
		{report.Position{File: "f", Column: 1}, "position with column", optionalColumn, true},
		{report.Location{File: "f"}, "location without pointer", optionalPointer, false},
		{report.Location{File: "f", Pointer: firstItemPointer}, "location with pointer", optionalPointer, true},
		{report.Location{File: "f"}, "location without argv index", optionalArgvIndex, false},
		{report.Location{File: "f", ArgvIndex: new(0)}, "location with argv index zero", optionalArgvIndex, true},
	}
}

func tableRecordCases() []optionalCase {
	return []optionalCase{
		{report.Part{}, "part without source", optionalSource, false},
		{report.Part{Source: new(0)}, "part with source", optionalSource, true},
		{report.Part{}, "part without style", optionalStyle, false},
		{report.Part{Style: new(0)}, "part with style", optionalStyle, true},
		{report.Source{Path: "p"}, "source without digest", optionalDigest, false},
		{report.Source{Path: "p", Digest: digestA}, "source with digest", optionalDigest, true},
		{report.Style{}, "style without text", optionalText, false},
		{report.Style{Text: &report.Text{}}, "style with text", optionalText, true},
	}
}

func queryRecordCases() []optionalCase {
	return []optionalCase{
		{report.GeneratedBrief{}, "brief without text", optionalText, false},
		{report.GeneratedBrief{Text: &report.TextBrief{}}, "brief with text", optionalText, true},
		{report.StyleDetail{}, "style detail without text", optionalText, false},
		{report.StyleDetail{Text: &report.TextDetail{}}, "style detail with text", optionalText, true},
		{report.PartView{}, "view without path", optionalPath, false},
		{report.PartView{Path: "/a.pdf"}, "view with path", optionalPath, true},
		{report.PartView{}, "view without generated", optionalGenerated, false},
		{report.PartView{Generated: &report.GeneratedBrief{}}, "view with generated", optionalGenerated, true},
		{report.PartView{}, "view without source", optionalSource, false},
		{report.PartView{Source: &report.Source{}}, "view with source", optionalSource, true},
		{report.PartView{}, "view without style", optionalStyle, false},
		{report.PartView{Style: &report.StyleDetail{}}, "view with style", optionalStyle, true},
	}
}

func responseCases() []optionalCase {
	return []optionalCase{
		{&report.ViewResponse[report.PartView]{}, "view response not oversized", optionalOversized, false},
		{&report.ViewResponse[report.PartView]{OversizedRecord: true}, "oversized view response", optionalOversized, true},
		{&report.Summary{}, "summary without omitted diagnostics", optionalOmitted, false},
		{&report.Summary{DiagnosticsOmitted: 3}, "summary with omitted diagnostics", optionalOmitted, true},
		{&report.Summary{}, "summary without a next command", optionalNext, false},
		{&report.Summary{Next: []string{programName}}, "summary with a next command", optionalNext, true},
	}
}

func TestOptionalMembersAreWrittenExactlyWhenPresent(t *testing.T) {
	t.Parallel()

	for _, group := range [][]optionalCase{locationCases(), tableRecordCases(), queryRecordCases(), responseCases()} {
		for _, tc := range group {
			if got := hasMember(t, tc.value, tc.member); got != tc.present {
				t.Errorf("%s: member %q present %v, want %v", tc.name, tc.member, got, tc.present)
			}
		}
	}
}
