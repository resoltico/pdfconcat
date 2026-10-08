// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
)

// boundaryCase edits a valid report in memory to sit exactly on a limit. A case with an empty pointer must
// validate; any other must be refused with that code at that pointer.
type boundaryCase struct {
	edit    func(r *report.Report)
	name    string
	pointer string
	code    report.Code
}

const (
	pointerCounts     = "/counts"
	pointerTextBounds = "/styles/0/text/bounds"
)

// incompleteLayout turns the valid check into a failed one whose layout stopped early, so only the
// per-part rules apply and the contiguity rules of a complete layout do not.
func incompleteLayout(r *report.Report) {
	r.Status = report.StatusFailed
	r.Phases.Layout, r.Phases.OutputVerification = report.PhaseIncomplete, report.PhaseNotRun
	r.Diagnostics = []report.Diagnostic{{Stage: fixtureLayoutStage, Code: "stopped", Message: "m"}}
}

func acceptedLimitCases() []boundaryCase {
	argvLocation := func(index int) func(r *report.Report) {
		return func(r *report.Report) {
			r.Status = report.StatusInvalid
			r.Diagnostics = []report.Diagnostic{
				{
					Stage:    fixtureUsageStage,
					Code:     fixtureBadFlagCode,
					Message:  "m",
					Location: &report.Location{File: argvFile, ArgvIndex: &index},
				},
			}
		}
	}

	return []boundaryCase{
		{func(r *report.Report) {
			r.Parts = nil
			r.Counts = report.Counts{SourcePages: new(int64(0)), GeneratedPages: new(int64(0)), TotalPages: new(int64(0))}
		}, "zero counts and no parts", "", ""},
		{argvLocation(0), "argv index zero", "", ""},
		{func(r *report.Report) {
			r.Styles[0].Size = report.PageSize{
				Origin: report.SizeExplicit,
				Width:  float64(assembly.MinPageSide),
				Height: float64(assembly.MinPageSide),
			}
		}, "smallest page", "", ""},
		{func(r *report.Report) {
			r.Styles[0].Text.Bounds = &report.Rect{}
		}, "empty text bounds", "", ""},
	}
}

func refusedLimitCases() []boundaryCase {
	return []boundaryCase{
		{func(r *report.Report) {
			r.Counts.GeneratedPages, r.Counts.SourcePages = new(int64(3)), new(int64(2))
		}, "generated count differs but the total adds up", pointerCounts, report.CodeInvalidRange},
		{func(r *report.Report) {
			r.Counts.SourcePages, r.Counts.TotalPages = new(int64(4)), new(int64(6))
		}, "total differs but the sum of the counts adds up", pointerCounts, report.CodeInvalidRange},
		{func(r *report.Report) {
			incompleteLayout(r)
			r.Parts[1].Range = &report.PageRange{Start: 3, End: 4}
		}, "ranges sharing one page", "/parts/1/range", report.CodeInvalidRange},
		{func(r *report.Report) {
			incompleteLayout(r)
			r.Parts[0].Range = nil
			r.Parts[1].ID = r.Parts[0].ID
		}, "duplicate id after a part without range", "/parts/1/id", report.CodeDuplicateID},
		{
			func(r *report.Report) { r.Styles[0].Text.Bounds.Height = -1 },
			"bounds height negative",
			pointerTextBounds,
			report.CodeInvalidValue,
		},
	}
}

func TestValidateAcceptsValuesOnTheirLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range acceptedLimitCases() {
		saved := mustDecode(t, completeCheck)
		tc.edit(saved)

		err := saved.Validate()
		if err != nil {
			t.Errorf(namedErrorFormat, tc.name, err)
		}
	}
}

func TestValidateRefusesValuesBeyondTheirLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range refusedLimitCases() {
		saved := mustDecode(t, completeCheck)
		tc.edit(saved)

		found, ok := report.AsError(saved.Validate())
		if !ok {
			t.Errorf("%s: accepted", tc.name)

			continue
		}

		located := found.Diagnostic.Location != nil && found.Diagnostic.Location.Pointer == tc.pointer
		if found.Diagnostic.Code != tc.code || !located {
			t.Errorf(namedErrorFormat, tc.name, found)
		}
	}
}

func TestReportRejectsInvalidComputedInkBounds(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, completeCheck)
	saved.Styles[0].Text.InkBounds = &report.Rect{Width: -1}
	err := saved.Validate()

	found, ok := report.AsError(err)
	if !ok || found.Diagnostic.Code != report.CodeInvalidValue || found.Diagnostic.Location.Pointer != "/styles/0/text/ink_bounds" {
		t.Fatalf("invalid ink bounds: %v", err)
	}
}

func TestComputedBoundsHaveNoInventedMagnitudeLimit(t *testing.T) {
	t.Parallel()
	saved := mustDecode(t, completeCheck)

	saved.Styles[0].Text.Bounds = &report.Rect{X: -1e12, Y: 1e12, Width: 1e12, Height: 1e12}
	if err := saved.Validate(); err != nil {
		t.Fatalf("finite computed geometry: %v", err)
	}

	encoded := encodeReport(t, saved)
	if err := mustDecode(t, encoded).Validate(); err != nil {
		t.Fatal(err)
	}
}
