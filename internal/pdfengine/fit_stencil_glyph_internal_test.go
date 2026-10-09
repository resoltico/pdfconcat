// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const fitUnpaintedGlyph = "0 0 d0 q Q"

func guardImage(t *testing.T, pdf *model.Context, stencil bool) types.IndirectRef {
	t.Helper()
	ref := guardStream(t, pdf, "", types.Dict{})
	entry, _ := pdf.FindTableEntry(ref.ObjectNumber.Value(), 0)

	stream, ok := entry.Object.(types.StreamDict)
	if !ok {
		t.Fatal("image is not stream")
	}

	stream.Dict[keySubtype] = types.Name(guardImageSubtype)
	stream.Dict["ImageMask"] = types.Boolean(stencil)

	return ref
}

func TestFitStencilPaintInvokesSelectedPattern(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	scope := types.Dict{}
	pattern := guardPattern(t, pdf, "/Pattern cs /P scn /Stencil Do", scope)
	scope[fitPattern] = types.Dict{"P": pattern}

	scope[keyXObject] = types.Dict{"Stencil": guardImage(t, pdf, true), "Photo": guardImage(t, pdf, false)}
	for _, paint := range []string{"/Stencil Do", "BI /W 8 /H 1 /IM true ID x EI"} {
		checkedErr0 := guardInspect(
			t,
			newFitProgramInspector(pdf),
			"/Pattern cs /P scn "+paint,
			scope,
		)
		if !errors.Is(
			checkedErr0,
			errFitUnsupported,
		) {
			t.Fatalf("stencil-only pattern cycle accepted: %v", checkedErr0)
		}
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), "/Pattern cs /P scn /Photo Do", scope); err != nil {
		t.Fatalf("ordinary image invented pattern execution: %v", err)
	}
}

func TestFitUsedGlyphUnknownBoundsAndD1Domain(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, program string
		accepted      bool
	}{
		{"d0 unpainted", fitUnpaintedGlyph, true},
		{"d0 unknown painted", "0 0 d0 0 0 10 10 re f", false},
		{"d1 declared", "0 0 0 0 10 10 d1 0 0 10 10 re f", true},
		{"missing metrics", emptyAppearanceDrawing, false},
		{"nonzero wy", "0 1 d0 q Q", false},
		{"d1 color", "0 0 0 0 10 10 d1 1 0 0 rg 0 0 10 10 re f", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := guardContext(t)
			glyph := guardStream(t, pdf, test.program, types.Dict{})
			font := guardType3(t, pdf, types.Dict{"A": glyph})
			entry, _ := pdf.FindTableEntry(font.ObjectNumber.Value(), 0)

			dict, ok := entry.Object.(types.Dict)
			if !ok {
				t.Fatal(guardFontDictionaryError)
			}

			dict["FontBBox"] = types.NewIntegerArray(0, 0, 0, 0)

			err := guardInspect(t, newFitProgramInspector(pdf), "/F 12 Tf (A) Tj", types.Dict{keyFont: types.Dict{"F": font}})
			if test.accepted && err != nil {
				t.Fatal(err)
			}

			if !test.accepted && !errors.Is(err, errFitUnsupported) {
				t.Fatalf("unproved glyph accepted: %v", err)
			}
		})
	}
}

func TestFitSimpleFontAdvanceKeepsLaterType3PositionKnown(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	glyph := guardStream(t, pdf, fitUnpaintedGlyph, types.Dict{})
	font := guardType3(t, pdf, types.Dict{"A": glyph})
	ordinary := types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name(nameType1), keyBaseFont: types.Name(fontHelvetica)}

	scope := types.Dict{keyFont: types.Dict{"Simple": ordinary, "Glyph": font}}

	checkedErr1 := guardInspect(
		t,
		newFitProgramInspector(pdf),
		"BT /Simple 12 Tf (A) Tj /Glyph 12 Tf (A) Tj ET",
		scope,
	)
	if checkedErr1 != nil {
		t.Fatalf("resolvable ordinary advance was discarded: %v", checkedErr1)
	}
}
