// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestVariableAppearanceFiniteDrawingLimits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		plan  variableAppearancePlan
		valid bool
	}{
		{"large-bordered-width", variableAppearancePlan{width: math.MaxFloat64, height: math.MaxFloat64 / 2, border: math.MaxFloat64 / 4, fontSize: 2}, true},
		{"large-bordered-height", variableAppearancePlan{width: math.MaxFloat64 / 2, height: math.MaxFloat64, border: math.MaxFloat64 / 4, fontSize: 2}, true},
		{"overflowing-list-leading", variableAppearancePlan{width: 20, height: 12, fontSize: math.MaxFloat64 / 1.05, kind: "list", lines: []variableAppearanceLine{{data: []byte("A"), width: .6}}}, false},
		{"overflowing-line-count", variableAppearancePlan{width: 20, height: 12, fontSize: math.MaxFloat64 / 2.05, kind: "list", lines: []variableAppearanceLine{{}, {}}}, false},
		{"right-text-position-overflow", variableAppearancePlan{width: 20, height: 12, border: math.MaxFloat64 / 2, fontSize: 1000, justification: 2, lines: []variableAppearanceLine{{width: math.MaxFloat64 / 1000}}}, false},
		{"comb-advance-overflow", variableAppearancePlan{width: 20, height: 12, fontSize: math.MaxFloat64 / 2, maxLen: 4, flags: variableCombFlag, glyphs: []variableAppearanceLine{{data: []byte("A"), width: 3}}}, false},
		{"large-comb-cell", variableAppearancePlan{width: math.MaxFloat64, height: 12, border: math.MaxFloat64 / 4, fontSize: 2, maxLen: 4, flags: variableCombFlag, glyphs: []variableAppearanceLine{{data: []byte("A"), width: .6}}}, true},
		{"comb-center-overflow", variableAppearancePlan{width: 20, height: 12, border: math.MaxFloat64 / 2, fontSize: 2, maxLen: 1, flags: variableCombFlag, glyphs: []variableAppearanceLine{{data: []byte("A"), width: math.MaxFloat64 / 2}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			pdf := signatureTestContext(t)
			_, err := test.plan.stream(t.Context(), pdf)
			if test.valid && err != nil || !test.valid && !errors.Is(err, errFormState) {
				t.Fatalf("finite drawing validity = %v, want %v: %v", err == nil, test.valid, err)
			}
		})
	}
}

func TestVariableAppearancePreservesBorderAndBackgroundGeometry(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, style, drawing string
		background, color    []float64
		border               float64
	}{
		{name: "no-paint"},
		{name: "zero-border", color: []float64{.25}},
		{name: "uncolored-border", border: 2},
		{name: "gray-background", background: []float64{.5}, drawing: "0.5 g\n0 0 20 12 re f\n"},
		{name: "RGB-background", background: []float64{.1, .2, .3}, drawing: "0.1 0.2 0.3 rg\n0 0 20 12 re f\n"},
		{name: "CMYK-background", background: []float64{.1, .2, .3, .4}, drawing: "0.1 0.2 0.3 0.4 k\n0 0 20 12 re f\n"},
		{name: "solid", border: 2, style: "S", color: []float64{.25}, drawing: "0.25 G\n2 w\n1 1 18 10 re s\n2 2 16 8 re W n\n"},
		{name: "dashed", border: 2, style: "D", color: []float64{.1, .2, .3}, drawing: "0.1 0.2 0.3 RG\n2 w\n[ 3 1] 0 d\n1 1 18 10 re s\n2 2 16 8 re W n\n"},
		{name: "underlined", border: 2, style: "U", color: []float64{.1, .2, .3, .4}, drawing: "0.1 0.2 0.3 0.4 K\n2 w\n0 0 m 20 0 l s\n2 2 16 8 re W n\n"},
		{name: "background-border", border: 2, style: "S", background: []float64{.5}, drawing: "0.5 g\n0 0 20 12 re f\n0.5 G\n2 w\n1 1 18 10 re s\n2 2 16 8 re W n\n"},
		{name: "beveled", border: 2, style: "B", color: []float64{.25}, drawing: "0.625 g\n0 0 m 0 12 l 20 12 l 18 10 l 2 10 l 2 2 l f\n0.125 g\n0 0 m 20 0 l 20 12 l 18 10 l 18 2 l 2 2 l f\n2 2 16 8 re W n\n"},
		{name: "inset", border: 2, style: "I", color: []float64{.25}, drawing: "0.125 g\n0 0 m 0 12 l 20 12 l 18 10 l 2 10 l 2 2 l f\n0.625 g\n0 0 m 20 0 l 20 12 l 18 10 l 18 2 l 2 2 l f\n2 2 16 8 re W n\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			plan := variableAppearancePlan{width: 20, height: 12, fontSize: 2, border: test.border, borderStyle: test.style, background: test.background, borderColor: test.color, dash: []float64{3, 1}}
			ref, err := plan.stream(t.Context(), pdf)
			if err != nil {
				t.Fatal(err)
			}
			stream := buttonStream(t, pdf, *ref)
			want := "q\n" + test.drawing + "q\nQ\nQ\n"
			if string(stream.Content) != want {
				t.Fatalf("drawing changed:\n%s\nwant:\n%s", stream.Content, want)
			}
		})
	}
}

func TestVariableAppearanceComboWithoutMaxLenPreservesCaption(t *testing.T) {
	t.Parallel()
	fixture := variableStyleSource(t, types.Dict{
		"FT": types.Name("Ch"), "Ff": types.Integer(variableComboFlag | variableCombFlag),
		"Opt": types.Array{types.StringLiteral("Display label")}, "V": types.StringLiteral("Display label"),
	})
	if _, err := prepareFormResources(t.Context(), fixture.pdf, 3); err != nil {
		t.Fatal(err)
	}
	appearance := buttonDictionary(t, fixture.pdf, fixture.widget["AP"])
	stream := buttonStream(t, fixture.pdf, appearance["N"])
	if !strings.Contains(string(stream.Content), "<446973706c6179206c6162656c> Tj") {
		t.Fatalf("accepted combo source lost its Display label caption: %s", stream.Content)
	}
}

func TestVariableAppearancePlacesTextFromGlyphAdvanceAndJustification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, drawing string
		justification int
	}{
		{name: "left", drawing: "4.00"},
		{name: "center", justification: 1, drawing: "9.40"},
		{name: "right", justification: 2, drawing: "14.80"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			font := signatureTestObject(pdf, types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name(nameType1), keyBaseFont: types.Name(fontHelvetica)})
			plan := variableAppearancePlan{resources: types.Dict{keyFont: types.Dict{"F1": font}}, width: 20, height: 12, border: 2, fontSize: 2, da: "/F1 2 Tf", maxLen: 4, justification: test.justification, lines: []variableAppearanceLine{{data: []byte("A"), width: .6}}}
			ref, err := plan.stream(t.Context(), pdf)
			if err != nil {
				t.Fatal(err)
			}
			stream := buttonStream(t, pdf, *ref)
			want := "q\nq\n/Tx BMC\nBT\n/F1 2 Tf\n1 0 0 1 " + test.drawing + " 5.20 Tm\n<41> Tj\nET\nEMC\nQ\nQ\n"
			if string(stream.Content) != want {
				t.Fatalf("text placement changed:\n%s\nwant:\n%s", stream.Content, want)
			}
		})
	}
}

func TestButtonAppearancePreservesBorderGeometryAndOnCaption(t *testing.T) {
	t.Parallel()
	for _, state := range []types.Name{buttonOffState, "Yes"} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			plan := buttonAppearancePlan{on: "Yes", background: [3]float64{.5, .6, .7}, borderColor: [3]float64{.1, .2, .3}, width: 20, height: 12, border: 1.25, fontSize: 3.5, textX: 4.25, textY: 2.75, caption: '4'}
			ref, err := plan.stream(pdf, state)
			if err != nil {
				t.Fatal(err)
			}
			stream := buttonStream(t, pdf, *ref)
			want := "0.5 0.6 0.7 rg\n0 0 20 12 re f\n1.25 w\n0.1 0.2 0.3 RG\n0.625 0.625 18.75 10.75 re s\n"
			if state == "Yes" {
				want += "q\nBT\n/ZaDb 3.50 Tf 0 g 1 0 0 1 4.25 2.75 Tm\n<34> Tj\nET\nQ\n"
			}
			if string(stream.Content) != want {
				t.Fatalf("button drawing changed:\n%s\nwant:\n%s", stream.Content, want)
			}
		})
	}
}

func TestVariableAppearancePreservesCombCellsAndListSelection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, kind, drawing  string
		flags, justification int
	}{
		{name: "comb-left", flags: variableCombFlag, drawing: "1 0 0 1 2.00 5.20 Tm\n1.40 0 Td\n<41> Tj\n3.80 0 Td\n<42> Tj\n"},
		{name: "comb-center", flags: variableCombFlag, justification: 1, drawing: "1 0 0 1 6.00 5.20 Tm\n1.40 0 Td\n<41> Tj\n3.80 0 Td\n<42> Tj\n"},
		{name: "comb-right", flags: variableCombFlag, justification: 2, drawing: "1 0 0 1 10.00 5.20 Tm\n1.40 0 Td\n<41> Tj\n3.80 0 Td\n<42> Tj\n"},
		{name: "list", kind: "list", drawing: "q\nBT\n/F1 2 Tf\n1 0 0 1 4.00 9.80 Tm\n<41> Tj\nET\nQ\nq\n0 g\n2 7.20 16 2.20 re f\nBT\n/F1 2 Tf\n1 0 0 1 4.00 7.60 Tm\n1 g\n<42> Tj\nET\nQ\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			font := signatureTestObject(pdf, types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name(nameType1), keyBaseFont: types.Name(fontHelvetica)})
			plan := variableAppearancePlan{resources: types.Dict{keyFont: types.Dict{"F1": font}}, width: 20, height: 12, border: 2, fontSize: 2, da: "/F1 2 Tf", maxLen: 4, flags: test.flags, justification: test.justification, kind: test.kind, selected: []bool{false, true}, lines: []variableAppearanceLine{{data: []byte("A"), width: .6}, {data: []byte("B"), width: .8}}, glyphs: []variableAppearanceLine{{data: []byte("A"), width: .6}, {data: []byte("B"), width: .8}}}
			ref, err := plan.stream(t.Context(), pdf)
			if err != nil {
				t.Fatal(err)
			}
			stream := buttonStream(t, pdf, *ref)
			want := "q\nq\n" + test.drawing + "Q\nQ\n"
			if test.kind != "list" {
				want = "q\nq\n/Tx BMC\nBT\n/F1 2 Tf\n" + test.drawing + "ET\nEMC\nQ\nQ\n"
			}
			if string(stream.Content) != want {
				t.Fatalf("cell or selection placement changed:\n%s\nwant:\n%s", stream.Content, want)
			}
		})
	}
}

func TestVariableAppearancePreservesMultilineOffsetsAndUnselectedRows(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, kind, drawing string
		flags               int
	}{
		{name: "multiline", flags: variableMultilineFlag, drawing: "/Tx BMC\nBT\n/F1 2 Tf\n1 0 0 1 0 9.00 Tm\n9.40 -2.00 Td\n<41> Tj\n-0.20 -2.00 Td\n<42> Tj\nET\nEMC\n"},
		{name: "unselected-list", kind: "list", drawing: "q\nBT\n/F1 2 Tf\n1 0 0 1 9.40 9.80 Tm\n<41> Tj\nET\nQ\nq\nBT\n/F1 2 Tf\n1 0 0 1 9.20 7.60 Tm\n<42> Tj\nET\nQ\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			plan := variableAppearancePlan{width: 20, height: 12, border: 2, fontSize: 2, da: "/F1 2 Tf", justification: 1, flags: test.flags, kind: test.kind, lines: []variableAppearanceLine{{data: []byte("A"), width: .6}, {data: []byte("B"), width: .8}}}
			ref, err := plan.stream(t.Context(), pdf)
			if err != nil {
				t.Fatal(err)
			}
			stream := buttonStream(t, pdf, *ref)
			want := "q\nq\n" + test.drawing + "Q\nQ\n"
			if string(stream.Content) != want {
				t.Fatalf("line placement changed:\n%s\nwant:\n%s", stream.Content, want)
			}
		})
	}
}
