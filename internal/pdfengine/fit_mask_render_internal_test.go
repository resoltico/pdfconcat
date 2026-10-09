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

func guardMaskDocument(t *testing.T, kind string) *model.Context {
	t.Helper()
	pdf := guardContext(t)
	page := guardPage(t, pdf)
	maskScope := types.Dict{keyExtGState: types.Dict{"Half": types.Dict{"ca": types.Float(0.5)}}}

	program := "0.5 g 0 0 100 100 re f"
	if kind == fitMaskAlpha {
		program = "/Half gs 0 0 100 100 re f"
	}

	mask := guardStream(t, pdf, program, maskScope)
	entry, _ := pdf.FindTableEntry(mask.ObjectNumber.Value(), 0)

	stream, ok := entry.Object.(types.StreamDict)
	if !ok {
		t.Fatal("mask is not stream")
	}

	group := types.Dict{"S": types.Name(fitTransparency)}
	if kind == guardLuminosityMask {
		group["CS"] = types.Name(fitDeviceRGB)
	}

	stream.Dict[fitGroup] = group
	scope := types.Dict{keyExtGState: types.Dict{"Mask": types.Dict{guardSoftMaskKey: types.Dict{"S": types.Name(kind), "G": mask}}}}
	page[keyResources] = scope
	page[keyMediaBox] = types.NewIntegerArray(0, 0, 100, 100)
	page[keyCropBox] = types.NewIntegerArray(0, 0, 100, 100)

	paint, err := pdf.NewStreamDictForBuf([]byte("/Mask gs 1 0 0 rg 0 0 100 100 re f"))
	if err != nil {
		t.Fatal(err)
	}

	reference, err := storeFitStream(t.Context(), pdf, paint)
	if err != nil {
		t.Fatal(err)
	}

	page[keyContents] = *reference

	return pdf
}

func TestFitSoftMaskIndependentRenderingBothTargets(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)

	for _, kind := range []string{fitMaskAlpha, guardLuminosityMask} {
		for _, target := range []PageSize{{210 * 72 / 25.4, 297 * 72 / 25.4}, {612, 1008}} {
			t.Run(kind, func(t *testing.T) {
				t.Parallel()
				guardMaskRenderCase(t, tools, kind, target)
			})
		}
	}
}

func guardMaskRenderCase(t *testing.T, tools pdforacle.Tools, kind string, target PageSize) {
	t.Helper()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, fitRenderSource)
	guardWriteDocument(t, guardMaskDocument(t, kind), sourcePath)

	fitted := guardMaskDocument(t, kind)
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
		if checked < 100 || wrong*100 > checked {
			t.Fatalf("soft mask color/placement changed: samples=%d mismatches=%d", checked, wrong)
		}
	}
}
