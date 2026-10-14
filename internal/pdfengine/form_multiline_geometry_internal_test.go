// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestMultilineBorderInsetsBoundDecodedAppearanceLines(t *testing.T) {
	t.Parallel()
	fixture := variableUnitAdvanceFixture(t)
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["DA"] = types.StringLiteral("/F1 10 Tf 0 g")
	fixture.parent["V"] = types.StringLiteral("AAA")
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 30, 100)
	plan := capturedVariablePlan(t, fixture)
	// Each declared glyph occupies 10 points. The 30-point box leaves 20 after
	// its two 3-point borders and 4-point text inset, so only two glyphs fit.
	if plan.fontSize != 10 || len(plan.lines) != 2 || string(plan.lines[0].data) != "AA" || plan.lines[0].width != 2 || string(plan.lines[1].data) != "A" || plan.lines[1].width != 1 {
		t.Fatalf("bordered multiline layout changed: %+v", plan)
	}
	assertVariableStreamContains(t, plan, "/F1 10 Tf", "<4141> Tj", "<41> Tj")
}

func TestFullyFittingMultilineSpacesRemainInDecodedAppearance(t *testing.T) {
	t.Parallel()
	fixture := variableUnitAdvanceFixture(t)
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["DA"] = types.StringLiteral("/F1 10 Tf 0 g")
	fixture.parent["V"] = types.StringLiteral("A A")
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 100, 100)
	plan := capturedVariablePlan(t, fixture)
	if len(plan.lines) != 1 || string(plan.lines[0].data) != "A A" {
		t.Fatalf("fitting interword space introduced a break: %+v", plan.lines)
	}
	assertVariableStreamContains(t, plan, "<412041> Tj")
}

func TestEmptyMultilineAutosizeRetainsOneParagraphAndMaximumSize(t *testing.T) {
	t.Parallel()
	fixture := variableUnitAdvanceFixture(t)
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["V"] = types.StringLiteral("")
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 100, 100)
	plan := capturedVariablePlan(t, fixture)
	if plan.fontSize != 20 || len(plan.lines) != 1 || len(plan.lines[0].data) != 0 {
		t.Fatalf("empty text lost its paragraph or maximum autosize: %+v", plan)
	}
	assertVariableStreamContains(t, plan, "/F1 20 Tf", "<> Tj")
}

func TestMultilineAutosizeHeightBoundaryCannotCoincideOnItsIEEEGrid(t *testing.T) {
	t.Parallel()
	// Successful text parsing/wrapping always leaves at least one line, even for
	// empty input. For every candidate size, both subtraction operands at a
	// positive height-fit result are >=size and hence multiples of ULP(size).
	// Their rounded difference remains on that grid, while .33*size does not.
	for reduction := range maximumMultilineAutoSize - 1 {
		size := float64(maximumMultilineAutoSize - reduction)
		right := .33 * size
		grid := math.Nextafter(size, math.Inf(1)) - size
		units := right / grid
		if units == math.Trunc(units) {
			t.Fatalf("size%g permits exact height-fit equality on grid%g", size, grid)
		}
		t.Logf("size%g right=%x grid=%x fractional_units=%g", size, math.Float64bits(right), math.Float64bits(grid), units-math.Trunc(units))
	}
}

func TestMultilineAutosizeKeepsPositiveMinimumInTinyAppearance(t *testing.T) {
	t.Parallel()
	fixture := variableUnitAdvanceFixture(t)
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.parent["V"] = types.StringLiteral("AAA")
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 40, 3.25)
	plan := capturedVariablePlan(t, fixture)
	if plan.fontSize != 1 || len(plan.lines) != 1 || string(plan.lines[0].data) != "AAA" {
		t.Fatalf("tiny appearance lost positive minimum fallback: %+v", plan)
	}
	assertVariableStreamContains(t, plan, "/F1 1 Tf", "<414141> Tj")
}
