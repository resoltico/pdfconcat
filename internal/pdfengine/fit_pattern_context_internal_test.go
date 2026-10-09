// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func guardPattern(t *testing.T, pdf *model.Context, content string, scope types.Dict) types.IndirectRef {
	t.Helper()
	ref := guardStream(t, pdf, content, scope)
	entry, _ := pdf.FindTableEntry(ref.ObjectNumber.Value(), 0)

	stream, ok := entry.Object.(types.StreamDict)
	if !ok {
		t.Fatal("pattern object is not stream")
	}

	stream.Dict["PatternType"] = types.Integer(1)
	stream.Dict["PaintType"] = types.Integer(1)
	stream.Dict["TilingType"] = types.Integer(1)
	stream.Dict["XStep"] = types.Integer(7)
	stream.Dict["YStep"] = types.Integer(11)
	stream.Dict[fitMatrix] = types.NewIntegerArray(1, 0, 0, 1, 2, 3)

	return ref
}

func TestFitPatternSelectionUsesImmutableEntryAnchor(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	scope := types.Dict{}
	pattern := guardPattern(t, pdf, "0 0 5 5 re f", types.Dict{})
	scope[fitPattern] = types.Dict{"P": pattern}
	fit := PageFit{Matrix: Affine{2, 0, 0, 2, 7, 11}, Original: PageSize{100, 100}, Target: PageSize{200, 200}, Scale: 2}

	for _, content := range []string{
		"/Pattern cs /P scn 3 0 0 4 13 17 cm 0 0 5 5 re f",
		"3 0 0 4 13 17 cm /Pattern cs /P scn 0 0 5 5 re f",
	} {
		i := newFitProgramInspector(pdf)

		page := types.Dict{keyContents: types.StreamDict{Dict: types.Dict{}, Content: []byte(content)}}
		if err := i.inspectPage(t.Context(), page, scope, fit); err != nil {
			t.Fatal(err)
		}

		found := false

		for result := range i.complete {
			if result.invocation.program == pattern.PDFString() {
				found = true

				if result.sourceMatrix != (Affine{1, 0, 0, 1, 2, 3}) || result.matrix != (Affine{2, 0, 0, 2, 11, 17}) {
					t.Fatalf("pattern inherited paint-time cm: original=%v emitted=%v", result.sourceMatrix, result.matrix)
				}
			}
		}

		if !found {
			t.Fatal("selected painted pattern was not visited")
		}
	}
}

func TestFitType3MissingMappedGlyphDoesNotInventNotdef(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	shared := guardStream(t, pdf, guardShowA, types.Dict{})
	notdef := guardStream(t, pdf, guardSharedGlyph, nil)
	font := guardType3(t, pdf, types.Dict{guardNotdef: notdef})

	scope := types.Dict{keyXObject: types.Dict{guardSharedName: shared}, keyFont: types.Dict{"F": font}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "/F 12 Tf /Shared Do", scope); err != nil {
		t.Fatalf("missing A invented cyclic .notdef: %v", err)
	}

	entry, _ := pdf.FindTableEntry(font.ObjectNumber.Value(), 0)

	dict, ok := entry.Object.(types.Dict)
	if !ok {
		t.Fatal(guardFontDictionaryError)
	}

	dict[keyEncoding] = types.Dict{keyDifferences: types.Array{types.Integer(65), types.Name(guardNotdef)}}

	if err := guardInspect(t, newFitProgramInspector(pdf), "/F 12 Tf /Shared Do", scope); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("explicit .notdef cycle accepted: %v", err)
	}
}

func TestFitResourceStreamReadDoesNotMarkValidationComplete(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	ref := guardStream(t, pdf, emptyAppearanceDrawing, types.Dict{})
	entry, _ := pdf.FindTableEntry(ref.ObjectNumber.Value(), 0)
	entry.Valid = false

	if _, err := fitReadProgramStream(t.Context(), pdf, ref); err != nil {
		t.Fatal(err)
	}

	if entry.Valid {
		t.Fatal("preflight caused later semantic validation to skip original stream")
	}
}

func TestFitCachedUnrestrictedProgramCannotApproveRestrictedColor(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	inspector.fit = PageFit{Matrix: fitIdentityMatrix(), Original: PageSize{100, 100}, Scale: 1}
	state := fitGraphicsState{
		matrix:            fitIdentityMatrix(),
		sourceMatrix:      fitIdentityMatrix(),
		textMatrix:        fitIdentityMatrix(),
		textLine:          fitIdentityMatrix(),
		horizontal:        1,
		textPositionKnown: true,
	}

	content := []byte("1 0 0 rg")
	if err := inspector.visit(t.Context(), "same-color-program", content, types.Dict{}, &state, "ordinary"); err != nil {
		t.Fatal(err)
	}

	state.colorRestricted = true
	if err := inspector.visit(t.Context(), "same-color-program", content, types.Dict{}, &state, "d1"); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("unrestricted cached result hid prohibited color: %v", err)
	}
}

func TestFitPatternCellUsesEntryFontInsteadOfPaintTimeFont(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	sharedScope := types.Dict{}
	patternScope := types.Dict{}
	pattern := guardPattern(t, pdf, guardShowA, patternScope)
	shared := guardStream(t, pdf, "/Safe 12 Tf /Pattern cs /P scn 0 0 10 10 re f", sharedScope)
	safeGlyph := guardStream(t, pdf, fitUnpaintedGlyph, types.Dict{})
	cyclicGlyph := guardStream(t, pdf, guardSharedGlyph, nil)
	safeFont := guardType3(t, pdf, types.Dict{"A": safeGlyph})
	cyclicFont := guardType3(t, pdf, types.Dict{"A": cyclicGlyph})
	scope := types.Dict{
		keyFont:    types.Dict{guardSafeFontName: safeFont, "Cyclic": cyclicFont},
		keyXObject: types.Dict{guardSharedName: shared},
		fitPattern: types.Dict{"P": pattern},
	}
	sharedScope[keyFont] = scope[keyFont]

	sharedScope[fitPattern] = scope[fitPattern]
	if err := guardInspect(t, newFitProgramInspector(pdf), "/Safe 12 Tf /Shared Do", scope); err != nil {
		t.Fatalf("valid initial font: %v", err)
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), "/Cyclic 12 Tf /Shared Do", scope); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("paint-time font hid entry-context Type3 cycle: %v", err)
	}
}

func TestFitD1AllowsFontOnlyGraphicsStateButRejectsOrdinaryImages(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	ordinary := types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name(nameType1), keyBaseFont: types.Name(fontHelvetica)}

	scope := types.Dict{
		keyExtGState: types.Dict{"FontOnly": types.Dict{keyFont: types.Array{ordinary, types.Integer(12)}}},
		keyXObject:   types.Dict{"Stencil": guardImage(t, pdf, true), "Photo": guardImage(t, pdf, false)},
	}
	for _, test := range []struct {
		program  string
		accepted bool
	}{{"0 0 0 0 1 1 d1 /FontOnly gs /Stencil Do", true}, {"0 0 0 0 1 1 d1 /Photo Do", false}} {
		glyph := guardStream(t, pdf, test.program, scope)
		font := guardType3(t, pdf, types.Dict{"A": glyph})
		entry, _ := pdf.FindTableEntry(font.ObjectNumber.Value(), 0)

		fontDict, ok := entry.Object.(types.Dict)
		if !ok {
			t.Fatal(guardFontDictionaryError)
		}

		fontDict[keyResources] = scope

		inspectionErr := guardInspect(t, newFitProgramInspector(pdf), "/F 12 Tf (A) Tj", types.Dict{keyFont: types.Dict{"F": font}})
		if test.accepted && inspectionErr != nil {
			t.Fatal(inspectionErr)
		}

		if !test.accepted && !errors.Is(inspectionErr, errFitUnsupported) {
			t.Fatalf("ordinary image under d1 accepted: %v", inspectionErr)
		}
	}
}
