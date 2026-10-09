// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func guardType3PatternDocument(t *testing.T, selection string) *model.Context {
	t.Helper()
	pdf := guardContext(t)
	page := guardPage(t, pdf)
	patternScope := types.Dict{}
	pattern := guardPattern(t, pdf, "1 0 0 rg 0 0 3 7 re f 0 0 1 rg 3 4 2 3 re f", types.Dict{})
	patternScope[fitPattern] = types.Dict{"P": pattern}
	patternEntry, _ := pdf.FindTableEntry(pattern.ObjectNumber.Value(), 0)

	patternStream, isPatternStream := patternEntry.Object.(types.StreamDict)
	if !isPatternStream {
		t.Fatal("pattern stream unavailable")
	}

	patternStream.Dict[fitMatrix] = types.NewIntegerArray(100, 0, 0, 100, 0, 0)
	glyphProgram := "500 0 d0 /Pattern cs /P scn 0 0 500 700 re f"
	paint := "BT /F 40 Tf 1 0 0 1 20 20 Tm (AA) Tj ET"

	if selection == "inherited" {
		glyphProgram = "500 0 0 0 500 700 d1 0 0 500 700 re f"
		paint = "/Pattern cs /P scn " + paint
	}

	glyph := guardStream(t, pdf, glyphProgram, patternScope)
	font := guardType3(t, pdf, types.Dict{"A": glyph})
	fontEntry, _ := pdf.FindTableEntry(font.ObjectNumber.Value(), 0)

	fontDict, ok := fontEntry.Object.(types.Dict)
	if !ok {
		t.Fatal("font unavailable")
	}

	fontDict[guardWidths] = types.NewIntegerArray(500)
	fontDict["Resources"] = patternScope
	fontDict["FontBBox"] = types.NewIntegerArray(0, 0, 500, 700)
	scope := types.Dict{keyFont: types.Dict{"F": font}, fitPattern: types.Dict{"P": pattern}}
	page[keyResources] = scope
	page[keyMediaBox] = types.NewIntegerArray(0, 0, 100, 100)
	page[keyCropBox] = types.NewIntegerArray(0, 0, 100, 100)

	stream, err := pdf.NewStreamDictForBuf([]byte(paint))
	if err != nil {
		t.Fatal(err)
	}

	ref, err := storeFitStream(t.Context(), pdf, stream)
	if err != nil {
		t.Fatal(err)
	}

	page[keyContents] = *ref

	return pdf
}

func TestFitType3PatternNamedRendererPreservation(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)

	for _, selection := range []string{"local", "inherited"} {
		for _, target := range []PageSize{{210 * 72 / 25.4, 297 * 72 / 25.4}, {612, 1008}} {
			t.Run(selection, func(t *testing.T) { t.Parallel(); guardType3PatternRenderCase(t, tools, selection, target) })
		}
	}
}

func guardType3PatternRenderCase(t *testing.T, tools pdforacle.Tools, selection string, target PageSize) {
	t.Helper()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, fitRenderSource)
	guardWriteDocument(t, guardType3PatternDocument(t, selection), sourcePath)

	fitted := guardType3PatternDocument(t, selection)
	if _, err := inspectPageFits(t.Context(), fitted, target); err != nil {
		t.Fatal(err)
	}

	fitStaticTestPages(t, fitted, target)

	fittedPath := filepath.Join(dir, fitRenderOutput)
	guardWriteDocument(t, fitted, fittedPath)

	for _, renderer := range []string{fitRendererPoppler, fitRendererMuPDF} {
		if renderer == fitRendererMuPDF && !pdforacle.HasMuPDF() {
			continue
		}

		sourceImage := guardRender(t, tools, sourcePath, renderer)
		fittedImage := guardRender(t, tools, fittedPath, renderer)

		checked, wrong := guardPatternAgreement(sourceImage, fittedImage, target)
		if checked < 50 || wrong*100 > checked {
			t.Fatalf("%s %s Type3 pattern changed: samples=%d mismatches=%d", renderer, selection, checked, wrong)
		}
	}
}
