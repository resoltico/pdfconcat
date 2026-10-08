// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"strings"
	"testing"
)

// checkedRequest describes a request over one three-page source (annotated, so it must be used whole)
// and a two-page resource document.
func checkedRequest(order []Run, expected int) *AssembleRequest {
	return &AssembleRequest{
		Resource:      &ResourceDocument{Path: "resource.pdf", Pages: 2},
		Destination:   "out.pdf",
		Sources:       []SourceFile{{Path: "source.pdf", Info: SourceInfo{Pages: 3, PageLocal: true}}},
		Order:         order,
		ExpectedPages: expected,
	}
}

func TestCheckAppliesThePageLimitExactly(t *testing.T) {
	t.Parallel()

	const half = MaxOutputPages / 2

	tests := map[string]struct {
		order   []Run
		wantErr bool
	}{
		"one run at the limit":               {[]Run{GeneratedPages(0, MaxOutputPages)}, false},
		"one run over the limit":             {[]Run{GeneratedPages(0, MaxOutputPages+1)}, true},
		"two runs at the limit":              {[]Run{GeneratedPages(0, MaxOutputPages-1), GeneratedPages(1, 1)}, false},
		"two runs over the limit":            {[]Run{GeneratedPages(0, MaxOutputPages-1), GeneratedPages(1, 2)}, true},
		"halves at the limit":                {[]Run{GeneratedPages(0, half), GeneratedPages(1, half)}, false},
		"halves over the limit":              {[]Run{GeneratedPages(0, half), GeneratedPages(1, half+1)}, true},
		"a full run followed by one page":    {[]Run{GeneratedPages(0, MaxOutputPages), GeneratedPages(1, 1)}, true},
		"small runs far below the limit":     {[]Run{GeneratedPages(0, 1), GeneratedPages(1, 1)}, false},
		"small runs followed by an overflow": {[]Run{GeneratedPages(0, 1), GeneratedPages(1, MaxOutputPages)}, true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			total := 0
			for _, run := range test.order {
				total += run.Count
			}

			// ExpectedPages equals the total, so only the limit can reject the request.
			err := checkedRequest(test.order, total).check()

			if test.wantErr != (err != nil) {
				t.Fatalf("check() = %v, want an error: %v", err, test.wantErr)
			}

			if test.wantErr && (CodeOf(err) != CodeRequestInvalid || !strings.Contains(err.Error(), "exceeds the limit")) {
				t.Fatalf("check() = %v", err)
			}
		})
	}
}

func TestCheckComparesTheTotalWithTheExpectedPages(t *testing.T) {
	t.Parallel()

	order := []Run{GeneratedPages(0, 2), SourcePages(0, 1, 3)}

	err := checkedRequest(order, 5).check()
	if err != nil {
		t.Fatalf("check() with the right total = %v", err)
	}

	for _, expected := range []int{4, 6, 0, -5} {
		err = checkedRequest(order, expected).check()
		if CodeOf(err) != CodeRequestInvalid || !strings.Contains(err.Error(), "5 pages, expected") {
			t.Errorf("check() expecting %d = %v", expected, err)
		}
	}
}

func TestCheckNamesTheValidRangeOfAnOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want  string
		order []Run
	}{
		"source index past the end":   {"not in 0..0", []Run{SourcePages(1, 1, 1)}},
		"source index below zero":     {"not in 0..0", []Run{SourcePages(-1, 1, 1)}},
		"resource index past the end": {"not in 0..1", []Run{GeneratedPages(2, 1)}},
		"resource index below zero":   {"not in 0..1", []Run{GeneratedPages(-1, 1)}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkedRequest(test.order, 1).check()
			if CodeOf(err) != CodeRequestInvalid || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("check() = %v, want a message containing %q", err, test.want)
			}
		})
	}

	empty := checkedRequest([]Run{SourcePages(0, 1, 1)}, 1)
	empty.Sources = nil

	err := empty.check()
	if err == nil || !strings.Contains(err.Error(), "not in 0..-1") {
		t.Fatalf("check() without sources = %v", err)
	}
}

func TestCheckRequiresWholeUseOfSourcesWithPageLocalObjects(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want    string
		run     Run
		wantErr bool
	}{
		"whole source":                      {"", SourcePages(0, 1, 3), false},
		"first pages only":                  {"pages 1-2 only", SourcePages(0, 1, 2), true},
		"first page only":                   {"pages 1-1 only", SourcePages(0, 1, 1), true},
		"last pages only":                   {"pages 2-3 only", SourcePages(0, 2, 2), true},
		"last page only":                    {"pages 3-3 only", SourcePages(0, 3, 1), true},
		"a range that starts late and ends": {"pages 2-2 only", SourcePages(0, 2, 1), true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkedRequest([]Run{test.run}, test.run.Count).check()

			if test.wantErr != (err != nil) {
				t.Fatalf("check() = %v, want an error: %v", err, test.wantErr)
			}

			if test.wantErr && (CodeOf(err) != CodePartialRange || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("check() = %v, want %s containing %q", err, CodePartialRange, test.want)
			}
		})
	}
}

func TestAssemblyPlanValidatesWithoutArtifactPaths(t *testing.T) {
	t.Parallel()

	plan := AssemblyPlan{
		Sources: []SourceFile{{Info: SourceInfo{Pages: 3}}}, GeneratedSpecs: 1,
		Order: []Run{SourcePages(0, 1, 3), GeneratedPages(0, 2)}, ExpectedPages: 5,
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}

	plan.ExpectedPages++

	failure := requirePlanFailure(t, plan)
	if failure.Reason != ReasonTotalMismatch || failure.Run != NoRun || failure.Source != NoSource {
		t.Fatalf("invariant failure: %+v", failure)
	}
}

func TestAssemblyPlanLocatesUnsupportedOccurrences(t *testing.T) {
	t.Parallel()

	sources := []SourceFile{
		{Info: SourceInfo{Pages: 1, LegacyDests: true}},
		{Info: SourceInfo{Pages: 1, LegacyDests: true}},
	}
	for _, second := range []int{0, 1} {
		plan := AssemblyPlan{Sources: sources, Order: []Run{SourcePages(0, 1, 1), SourcePages(second, 1, 1)}, ExpectedPages: 2}

		failure := requirePlanFailure(t, plan)
		if failure.Code != CodeLegacyDestsRepeated || failure.Reason != ReasonLegacyDests || failure.Run != 1 || failure.Source != second {
			t.Fatalf("second legacy occurrence: %+v", failure)
		}
	}
}

func TestAssemblyPlanLocatesPageLimitWithoutExpandingPages(t *testing.T) {
	t.Parallel()

	plan := AssemblyPlan{
		Sources: []SourceFile{{Info: SourceInfo{Pages: MaxOutputPages}}}, GeneratedSpecs: 1,
		Order: []Run{SourcePages(0, 1, MaxOutputPages), GeneratedPages(0, 1)}, ExpectedPages: MaxOutputPages + 1,
	}

	failure := requirePlanFailure(t, plan)
	if failure.Reason != ReasonPageLimit || failure.Run != 1 || failure.Source != NoSource {
		t.Fatalf("page limit: %+v", failure)
	}

	plan.Order, plan.ExpectedPages = plan.Order[:1], MaxOutputPages
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
}

func requirePlanFailure(t *testing.T, plan AssemblyPlan) *Error {
	t.Helper()

	failure, ok := errors.AsType[*Error](plan.Validate())
	if !ok {
		t.Fatal("expected a structured assembly-plan failure")
	}

	return failure
}

func TestAssemblyPlanRejectsNegativeGeneratedSpecCount(t *testing.T) {
	t.Parallel()

	plan := AssemblyPlan{GeneratedSpecs: -1, Order: []Run{GeneratedPages(0, 1)}, ExpectedPages: 1}

	failure := requirePlanFailure(t, plan)
	if failure.Reason != ReasonResource || failure.Run != NoRun || failure.Source != NoSource {
		t.Fatalf("negative specification count: %+v", failure)
	}
}
