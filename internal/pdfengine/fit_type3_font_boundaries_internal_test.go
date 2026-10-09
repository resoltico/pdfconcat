// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitType3ShownFontMetadataRefusesAndUnusedFontDoesNot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		change func(types.Dict)
		name   string
	}{
		{name: "invalid-glyph-resource-scope", change: func(font types.Dict) { font[keyResources] = types.Integer(1) }},
		{name: "invalid-glyph-declared-box", change: func(font types.Dict) { font["FontBBox"] = types.Integer(1) }},
		{name: "missing-encoding", change: func(font types.Dict) { delete(font, keyEncoding) }},
		{name: "missing-font-matrix", change: func(font types.Dict) { delete(font, guardFontMatrix) }},
		{name: "invalid-font-matrix", change: func(font types.Dict) { font[guardFontMatrix] = types.NewIntegerArray(1, 0, 0) }},
		{name: "nondictionary-char-procs", change: func(font types.Dict) { font[guardCharProcs] = types.Integer(1) }},
		{name: "invalid-first-char", change: func(font types.Dict) { font[guardFirstChar] = types.Name(guardBadMetadata) }},
		{name: "invalid-last-char", change: func(font types.Dict) { font[guardLastChar] = types.Name(guardBadMetadata) }},
		{name: "negative-character-range", change: func(font types.Dict) { font[guardFirstChar] = types.Integer(-1) }},
		{name: "reversed-character-range", change: func(font types.Dict) { font[guardFirstChar] = types.Integer(66) }},
		{name: "character-range-overflow", change: func(font types.Dict) { font[guardLastChar] = types.Integer(256) }},
		{name: "missing-width", change: func(font types.Dict) { font[guardWidths] = types.Array{} }},
		{name: "non-array-widths", change: func(font types.Dict) { font[guardWidths] = types.Integer(1) }},
		{name: "nonnumeric-width", change: func(font types.Dict) { font[guardWidths] = types.Array{types.Name(guardBadMetadata)} }},
		{name: "nonfinite-width", change: func(font types.Dict) { font[guardWidths] = types.Array{types.Float(math.Inf(1))} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			glyph := guardStream(t, pdf, guardZeroGlyphMetrics, nil)
			ref := guardType3(t, pdf, types.Dict{"A": glyph})
			entry, _ := pdf.FindTableEntry(ref.ObjectNumber.Value(), 0)

			font, isFont := entry.Object.(types.Dict)
			if !isFont {
				t.Fatal(guardFontDictionaryError)
			}

			test.change(font)

			scope := types.Dict{keyFont: types.Dict{"F": ref}}
			if err := guardInspect(t, newFitProgramInspector(pdf), "BT /F 12 Tf", scope); err != nil {
				t.Fatalf("unshown Type3 metadata rejected: %v", err)
			}

			if err := guardInspect(t, newFitProgramInspector(pdf), guardShowFontA, scope); err == nil {
				t.Fatal("shown malformed Type3 font metadata accepted")
			}
		})
	}
}

func TestFitType3UsedCharProcRejectsMalformedProgramAndMetrics(t *testing.T) {
	t.Parallel()

	for n, program := range []string{"", "q", "1 0 d0", "0 1 d0", "0 0 1 1 0 0 d1", "BI /W 1 ID", "0 0 d0 /Absent Do"} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			glyph := guardStream(t, pdf, program, nil)
			font := guardType3(t, pdf, types.Dict{"A": glyph})

			scope := types.Dict{keyFont: types.Dict{"F": font}}
			if err := guardInspect(t, newFitProgramInspector(pdf), guardShowFontA, scope); err == nil {
				t.Fatal("malformed used Type3 CharProc or metrics accepted")
			}
		})
	}
}

func TestFitType3UnmappedCodeDoesNotInventNotdefAndOutOfRangeWidthIsZero(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	cycle := guardStream(t, pdf, "0 0 d0 /Cycle Do", nil)
	font := guardType3(t, pdf, types.Dict{guardNotdef: cycle})
	scope := types.Dict{keyFont: types.Dict{"F": font}, keyXObject: types.Dict{"Cycle": cycle}}
	state := executeFitTextProgram(t, newFitProgramInspector(pdf), scope, "BT /F 12 Tf 1 0 0 1 10 20 Tm (B) Tj")
	assertFitTextPosition(t, state, 10, 20)
}

func TestFitType3FiniteWidthAndFontSizeCannotOverflowAdvance(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	font := guardType3(t, pdf, types.Dict{})
	entry, _ := pdf.FindTableEntry(font.ObjectNumber.Value(), 0)

	dict, isFont := entry.Object.(types.Dict)
	if !isFont {
		t.Fatal(guardFontDictionaryError)
	}

	dict[guardWidths] = types.Array{types.Float(1e308)}

	scope := types.Dict{keyFont: types.Dict{"F": font}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "BT /F 14000 Tf (A) Tj", scope); !errors.Is(err, errFitGeometry) {
		t.Fatalf("finite Type3 metrics produced unsafe horizontal advance: %v", err)
	}
}

func TestFitShownGlyphFiniteTmRefusesMatrixArithmeticOverflow(t *testing.T) {
	t.Parallel()

	huge := "1" + strings.Repeat("0", 308) + ".0"
	program := "BT /F 14000 Tf " + huge + " 0 0 1 0 0 Tm (A) Tj"
	pdf := guardContext(t)
	glyph := guardStream(t, pdf, guardZeroGlyphMetrics, nil)
	font := guardType3(t, pdf, types.Dict{"A": glyph})

	scope := types.Dict{keyFont: types.Dict{"F": font}}
	if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); !errors.Is(err, errFitGeometry) {
		t.Fatalf("shown glyph matrix overflow must be located geometry refusal: %v", err)
	}
}
