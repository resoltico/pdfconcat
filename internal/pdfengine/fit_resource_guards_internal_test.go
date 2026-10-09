// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const (
	guardLeafName           = "Leaf"
	guardSinglePageTree     = "<< /Type /Pages /Count 1 /Kids [3 0 R] >>"
	guardSafeFontName       = "Safe"
	guardFirstSourcePage    = "source page 1"
	guardLiteralDeviceName  = "DeviceR#47B"
	guardNotdef             = ".notdef"
	guardZeroGlyphMetrics   = "0 0 d0"
	guardPaintShading       = "/S sh"
	guardInvalidName        = "Invalid"
	guardApplyGraphicsState = "/G gs"
	guardImageSubtype       = "Image"
	guardPaintPattern       = "/Pattern cs /P scn 0 0 1 1 re f"
	guardShowFontA          = "BT /F 12 Tf (A) Tj"
	guardSoftMaskKey        = "SMask"
	guardLuminosityMask     = "Luminosity"
	guardSelectPattern      = "/Pattern cs /P scn"
	guardBadMetadata        = "bad"
	guardMissingWidth       = "MissingWidth"
	guardUnknownFont        = "Unknown"
	guardColorAlias         = "Alias"
	guardPatternType        = "PatternType"
	guardPaintType          = "PaintType"
	guardSelfDo             = "/Self Do"
	guardType3Name          = "Type3"
	guardFontMatrix         = "FontMatrix"
	guardCharProcs          = "CharProcs"
	guardFirstChar          = "FirstChar"
	guardLastChar           = "LastChar"
	guardWidths             = "Widths"

	guardShowA               = "(A) Tj"
	guardSharedGlyph         = "0 0 d0 /Shared Do"
	guardSharedName          = "Shared"
	guardFontDictionaryError = "font is not dictionary"
)

func guardContext(t *testing.T) *model.Context {
	t.Helper()
	return readUnvalidated(t, pdffixture.Plain("guard"))
}

func guardStream(t *testing.T, pdf *model.Context, content string, resources types.Dict) types.IndirectRef {
	t.Helper()

	stream, err := pdf.NewStreamDictForBuf([]byte(content))
	if err != nil {
		t.Fatal(err)
	}

	stream.Dict[keyType] = types.Name(keyXObject)
	stream.Dict[keySubtype] = types.Name(formXObjectSubtype)

	stream.Dict[keyBBox] = types.NewIntegerArray(0, 0, 100, 100)
	if resources != nil {
		stream.Dict[keyResources] = resources
	}

	ref, err := storeFitStream(t.Context(), pdf, stream)
	if err != nil {
		t.Fatal(err)
	}

	return *ref
}

func guardInspect(t *testing.T, i *fitProgramInspector, content string, resources types.Dict) error {
	t.Helper()

	return i.inspectPage(
		t.Context(),
		types.Dict{keyContents: types.StreamDict{Dict: types.Dict{}, Content: []byte(content)}},
		resources,
		PageFit{
			Matrix:   fitIdentityMatrix(),
			Visible:  [4]float64{0, 0, 100, 100},
			Original: PageSize{100, 100},
			Target:   PageSize{100, 100},
			Scale:    1,
			UserUnit: 1,
		},
	)
}

func guardType3(t *testing.T, pdf *model.Context, glyphs types.Dict) types.IndirectRef {
	t.Helper()

	dict := types.Dict{
		keyType:         types.Name(keyFont),
		keySubtype:      types.Name(guardType3Name),
		"FontBBox":      types.NewIntegerArray(0, 0, 100, 100),
		guardFontMatrix: types.NewNumberArray(0.001, 0, 0, 0.001, 0, 0),
		keyEncoding:     types.Dict{keyDifferences: types.Array{types.Integer(65), types.Name("A")}},
		guardCharProcs:  glyphs,
		guardFirstChar:  types.Integer(65),
		guardLastChar:   types.Integer(65),
		guardWidths:     types.NewIntegerArray(0),
	}

	ref, err := pdf.IndRefForNewObject(dict)
	if err != nil {
		t.Fatal(err)
	}

	return *ref
}

func TestFitResourceCyclesFollowExecutionNotOwnership(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	scope := types.Dict{}
	self := guardStream(t, pdf, guardSelfDo, scope)

	scope[keyXObject] = types.Dict{"Self": self}
	if err := guardInspect(t, newFitProgramInspector(pdf), emptyAppearanceDrawing, scope); err != nil {
		t.Fatalf("unused self ownership: %v", err)
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), guardSelfDo, scope); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("executing self cycle accepted: %v", err)
	}

	aScope, bScope := types.Dict{}, types.Dict{}
	a := guardStream(t, pdf, "/B Do", aScope)
	b := guardStream(t, pdf, "/A Do", bScope)
	aScope[keyXObject] = types.Dict{"B": b}

	bScope[keyXObject] = types.Dict{"A": a}

	checkedErr0 := guardInspect(
		t,
		newFitProgramInspector(pdf),
		"/A Do",
		types.Dict{keyXObject: types.Dict{"A": a}},
	)
	if !errors.Is(
		checkedErr0,
		errFitUnsupported,
	) {
		t.Fatalf("executing mutual cycle accepted: %v", checkedErr0)
	}
}

func TestFitResourceSharedDAGIsMemoized(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)

	leaf := guardStream(t, pdf, emptyAppearanceDrawing, types.Dict{})
	for range 20 {
		leaf = guardStream(t, pdf, "/Child Do /Child Do", types.Dict{keyXObject: types.Dict{"Child": leaf}})
	}

	inspector := newFitProgramInspector(pdf)
	if err := guardInspect(t, inspector, "/Root Do /Root Do", types.Dict{keyXObject: types.Dict{"Root": leaf}}); err != nil {
		t.Fatal(err)
	}

	if inspector.operations > 44 || inspector.edges > 44 {
		t.Fatalf("acyclic fanout expanded instead of shared: operations=%d edges=%d", inspector.operations, inspector.edges)
	}
}

func TestFitResourceInheritedFontContextCannotHideType3Cycle(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	shared := guardStream(t, pdf, guardShowA, types.Dict{})
	safe := guardStream(t, pdf, fitUnpaintedGlyph, types.Dict{})
	cyclic := guardStream(t, pdf, guardSharedGlyph, nil)
	safeFont := guardType3(t, pdf, types.Dict{"A": safe})
	cyclicFont := guardType3(t, pdf, types.Dict{"A": cyclic})

	scope := types.Dict{
		keyXObject: types.Dict{guardSharedName: shared},
		keyFont:    types.Dict{guardSafeFontName: safeFont, "Cyclic": cyclicFont},
	}
	if err := guardInspect(t, newFitProgramInspector(pdf), "/Cyclic 12 Tf", scope); err != nil {
		t.Fatalf("unshown glyph: %v", err)
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), "/Safe 12 Tf /Shared Do", scope); err != nil {
		t.Fatalf("safe shown glyph: %v", err)
	}

	checkedErr1 := guardInspect(
		t,
		newFitProgramInspector(pdf),
		"/Safe 12 Tf /Shared Do /Cyclic 12 Tf /Shared Do",
		scope,
	)
	if !errors.Is(
		checkedErr1,
		errFitUnsupported,
	) {
		t.Fatalf("object-only cache hid incoming font cycle: %v", checkedErr1)
	}
}

func TestFitResourcePatternIsCalledOnlyWhenPainted(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	scope := types.Dict{}
	pattern := guardStream(t, pdf, "/Pattern cs /P scn 0 0 10 10 re f", scope)
	entry, _ := pdf.FindTableEntry(pattern.ObjectNumber.Value(), 0)

	stream, ok := entry.Object.(types.StreamDict)
	if !ok {
		t.Fatal("fixture stored object is not stream")
	}

	stream.Dict["PatternType"] = types.Integer(1)
	stream.Dict["PaintType"] = types.Integer(1)
	stream.Dict["TilingType"] = types.Integer(1)
	stream.Dict["XStep"] = types.Integer(10)
	stream.Dict["YStep"] = types.Integer(10)

	scope[fitPattern] = types.Dict{"P": pattern}
	if err := guardInspect(t, newFitProgramInspector(pdf), guardSelectPattern, scope); err != nil {
		t.Fatalf("unpainted pattern: %v", err)
	}

	checkedErr2 := guardInspect(
		t,
		newFitProgramInspector(pdf),
		"/Pattern cs /P scn 0 0 10 10 re f",
		scope,
	)
	if !errors.Is(
		checkedErr2,
		errFitUnsupported,
	) {
		t.Fatalf("painted pattern cycle accepted: %v", checkedErr2)
	}
}

func TestFitResourceMasksFollowAppliedState(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	scope := types.Dict{}
	form := guardStream(t, pdf, "/Mask gs", scope)
	entry, _ := pdf.FindTableEntry(form.ObjectNumber.Value(), 0)

	stream, ok := entry.Object.(types.StreamDict)
	if !ok {
		t.Fatal("fixture stored object is not stream")
	}

	stream.Dict[fitGroup] = types.Dict{"S": types.Name(fitTransparency)}

	scope[keyExtGState] = types.Dict{"Mask": types.Dict{guardSoftMaskKey: types.Dict{"S": types.Name(fitMaskAlpha), "G": form}}}
	if err := guardInspect(t, newFitProgramInspector(pdf), emptyAppearanceDrawing, scope); err != nil {
		t.Fatalf("unselected mask: %v", err)
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), "/Mask gs", scope); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("applied mask cycle accepted: %v", err)
	}
}

func TestFitResourceEscapedNamesAndCancellation(t *testing.T) {
	t.Parallel()
	pdf := guardContext(t)
	leaf := guardStream(t, pdf, emptyAppearanceDrawing, types.Dict{})

	scope := types.Dict{keyXObject: types.Dict{"Leaf1": leaf}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "/Leaf#31 Do", scope); err != nil {
		t.Fatalf("escaped name lost: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := newFitProgramInspector(pdf).inspect(ctx, []byte(emptyAppearanceDrawing), scope, fitIdentityMatrix()); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled visitor: %v", err)
	}
}

func TestFitInvokedFormMetadataFailureRetainsResourceObjectAndProgramSpan(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	ref := guardStream(t, pdf, "", nil)
	entry, _ := pdf.FindTableEntry(ref.ObjectNumber.Value(), 0)

	stream, isStream := entry.Object.(types.StreamDict)
	if !isStream {
		t.Fatal("form fixture is not a stream")
	}

	stream.Dict[keyBBox] = types.NewIntegerArray(10, 0, 0, 10)
	scope := types.Dict{keyXObject: types.Dict{"Broken": ref}}

	err := guardInspect(t, newFitProgramInspector(pdf), "/Broken Do", scope)
	if err == nil {
		t.Fatal("invalid used Form BBox accepted")
	}

	for _, locator := range []string{"/XObject /Broken", ref.PDFString(), "byte 0:", keyBBox} {
		if !strings.Contains(err.Error(), locator) {
			t.Fatalf("resource metadata refusal lost %q: %v", locator, err)
		}
	}
}
