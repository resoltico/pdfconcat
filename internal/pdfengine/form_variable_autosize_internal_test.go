// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Declared one-em advances make these geometry expectations independent of the
// core-font metric tables and of the autosizing calculation under test.
func variableUnitAdvanceFixture(t *testing.T) *formFixture {
	t.Helper()
	fixture := variableTestFixture()
	fixture.form["Q"] = types.Integer(0)
	fixture.parent["V"] = types.StringLiteral("AA")
	fixture.parent["DA"] = types.StringLiteral(variableAutoDA)
	resources := buttonDictionary(t, fixture.pdf, fixture.form["DR"])
	fonts := buttonDictionary(t, fixture.pdf, resources[keyFont])
	fonts["F1"] = types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name(type1FontSubtype), keyBaseFont: types.Name(fontHelvetica), "FirstChar": types.Integer(65), "Widths": types.Array{types.Integer(1000), types.Integer(1000)}}
	fixture.widget["BS"] = types.Dict{"W": types.Integer(3)}
	return fixture
}

func assertVariableStreamContains(t *testing.T, plan *variableAppearancePlan, wants ...string) {
	t.Helper()
	pdf := signatureTestContext(t)
	ref, err := plan.stream(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}
	content := string(buttonStream(t, pdf, *ref).Content)
	for _, want := range wants {
		if !strings.Contains(content, want) {
			t.Fatalf("decoded appearance lacks %q:\n%s", want, content)
		}
	}
}

func TestVariableAutosizeHonorsBorderInsetsInDecodedAppearance(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                    string
		width, height, wantSize float64
		flags, maxLen           int
		wantPosition            string
	}{
		{name: "text height", width: 100, height: 25, wantSize: 19, wantPosition: "1 0 0 1 5.00 4.90 Tm"},
		{name: "text width", width: 46, height: 100, wantSize: 18, wantPosition: "1 0 0 1 5.00 42.80 Tm"},
		{name: "comb height", width: 200, height: 25, wantSize: 19, flags: variableCombFlag, maxLen: 4, wantPosition: "1 0 0 1 3.00 4.90 Tm"},
		{name: "comb width", width: 50, height: 100, wantSize: 11, flags: variableCombFlag, maxLen: 4, wantPosition: "1 0 0 1 3.00 45.60 Tm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := variableUnitAdvanceFixture(t)
			fixture.parent["Ff"] = types.Integer(test.flags)
			fixture.parent[keyMaxLen] = types.Integer(test.maxLen)
			fixture.widget[keyRect] = types.NewNumberArray(0, 0, test.width, test.height)
			plan := capturedVariablePlan(t, fixture)
			if plan.fontSize != test.wantSize {
				t.Fatalf("font size=%v, want %v", plan.fontSize, test.wantSize)
			}
			wantFont := fmt.Sprintf("/F1 %g Tf", test.wantSize)
			assertVariableStreamContains(t, plan, wantFont, test.wantPosition)
		})
	}
}

func TestEmptyVariableAutosizeDoesNotDivideByZeroAdvance(t *testing.T) {
	t.Parallel()
	fixture := variableUnitAdvanceFixture(t)
	fixture.parent["V"] = types.StringLiteral("")
	fixture.widget["BS"] = types.Dict{"W": types.Integer(1)}
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 6, 40)
	plan := capturedVariablePlan(t, fixture)
	if plan.fontSize != 38 {
		t.Fatalf("empty field font size=%v, want38", plan.fontSize)
	}
	assertVariableStreamContains(t, plan, "/F1 38 Tf", "<> Tj")
}

func TestVariableCompilationRejectsZeroDrawingSizeAndEmptyCombWithoutCells(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"zero drawing size", "zero comb cells"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := variableUnitAdvanceFixture(t)
			if name == "zero drawing size" {
				fixture.widget[keyRect] = types.NewNumberArray(0, 0, 10, 40)
			} else {
				fixture.parent["Ff"] = types.Integer(variableCombFlag)
				fixture.parent[keyMaxLen] = types.Integer(0)
				fixture.parent["V"] = types.StringLiteral("")
			}
			if _, err := analyzeForm(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
				t.Fatalf("invalid geometry accepted: %v", err)
			}
		})
	}
}

func TestMultilineVariableExactWidthRetainsBothGlyphsAndDrawingFont(t *testing.T) {
	t.Parallel()
	fixture := variableUnitAdvanceFixture(t)
	fixture.parent["V"] = types.StringLiteral("AB")
	fixture.parent["DA"] = types.StringLiteral("/F1 12 Tf")
	fixture.parent["Ff"] = types.Integer(variableMultilineFlag)
	fixture.widget[keyRect] = types.NewNumberArray(0, 0, 34, 40)
	plan := capturedVariablePlan(t, fixture)
	if len(plan.lines) != 1 || string(plan.lines[0].data) != "AB" {
		t.Fatalf("exact fit was split: %+v", plan.lines)
	}
	assertVariableStreamContains(t, plan, "/F1 12 Tf", "<4142> Tj")
}

func TestVariableDrawingValidatesFiniteProductsFromDeclaredSourceWidths(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		size   string
		reject bool
	}{{size: "100"}, {size: "10000", reject: true}} {
		t.Run(test.size, func(t *testing.T) {
			t.Parallel()
			fixture := variableUnitAdvanceFixture(t)
			fixture.parent["V"] = types.StringLiteral("A")
			fixture.parent["DA"] = types.StringLiteral("/F1 " + test.size + " Tf")
			resources := buttonDictionary(t, fixture.pdf, fixture.form["DR"])
			fonts := buttonDictionary(t, fixture.pdf, resources[keyFont])
			font := buttonDictionary(t, fixture.pdf, fonts["F1"])
			font["Widths"] = types.Array{types.Float(math.MaxFloat64)}
			_, err := analyzeForm(t.Context(), fixture.pdf)
			if test.reject {
				if !errors.Is(err, errFormState) {
					t.Fatalf("overflowing source advance accepted: %v", err)
				}
			} else if err != nil {
				t.Fatalf("large finite source advance rejected: %v", err)
			}
		})
	}
}
