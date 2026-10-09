// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"image"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

const (
	fitRendererPoppler = "poppler"
	fitRendererMuPDF   = "mupdf"
	fitRenderSource    = "source.pdf"
	fitRenderOutput    = "fitted.pdf"
)

func guardPage(t *testing.T, pdf *model.Context) types.Dict {
	t.Helper()

	root, err := pdf.PagesContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var page types.Dict

	err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, dict types.Dict, _ inheritedAttrs) error { page = dict; return nil })
	if err != nil {
		t.Fatal(err)
	}

	if page == nil {
		t.Fatal("no fixture page")
	}

	return page
}

func guardPatternDocument(t *testing.T, paintType int) *model.Context {
	t.Helper()
	pdf := guardContext(t)
	page := guardPage(t, pdf)
	scope := types.Dict{}
	cell := "1 0 0 rg 0 0 3 7 re f 0 0 1 rg 3 4 2 3 re f"
	content := guardSelectPattern

	if paintType == 2 {
		cell = "0 0 3 7 re f"
		content = "/PCS cs 0 1 0 /P scn"
		scope[fitColorSpace] = types.Dict{"PCS": types.Array{types.Name(fitPattern), types.Name(fitDeviceRGB)}}
	}

	pattern := guardPattern(t, pdf, cell, types.Dict{})
	if paintType == 2 {
		entry, _ := pdf.FindTableEntry(pattern.ObjectNumber.Value(), 0)

		stream, ok := entry.Object.(types.StreamDict)
		if !ok {
			t.Fatal("pattern stream unavailable")
		}

		stream.Dict["PaintType"] = types.Integer(2)
	}

	scope[fitPattern] = types.Dict{"P": pattern}
	page[keyResources] = scope
	page[keyMediaBox] = types.NewIntegerArray(0, 0, 100, 100)
	page[keyCropBox] = types.NewIntegerArray(0, 0, 100, 100)

	stream, err := pdf.NewStreamDictForBuf([]byte(content + " 1 0 0 1 13 17 cm -13 -17 100 100 re f"))
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

func guardWriteDocument(t *testing.T, pdf *model.Context, path string) {
	t.Helper()

	p := pool{pdf: pdf}
	if _, err := p.write(t.Context(), path); err != nil {
		t.Fatal(err)
	}
}

func guardRender(t *testing.T, tools pdforacle.Tools, path, renderer string) image.Image {
	t.Helper()

	document, err := pdforacle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}

	var img image.Image
	if renderer == fitRendererPoppler {
		img, err = document.Raster(1)
	} else {
		img, err = document.MuPDFRaster(1)
	}

	if err != nil {
		t.Fatal(err)
	}

	return img
}

func guardRGB(img image.Image, x, y int) [3]uint8 {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]uint8{uint8((r >> 8) & 0xff), uint8((g >> 8) & 0xff), uint8((b >> 8) & 0xff)}
}

func guardComparePattern(t *testing.T, source, fitted image.Image, target PageSize) {
	t.Helper()

	checked, wrong := guardPatternAgreement(source, fitted, target)
	if checked < 100 || wrong*100 > checked {
		t.Fatalf("source pattern phase/color changed: compared=%d mismatched=%d", checked, wrong)
	}

	incorrect := target
	incorrect.Width *= 0.9

	negativeCount, negativeWrong := guardPatternAgreement(source, fitted, incorrect)
	if negativeCount < 100 || negativeWrong*100 <= negativeCount {
		t.Fatalf("wrong placement negative control not detected: compared=%d mismatched=%d", negativeCount, negativeWrong)
	}
}

func guardPatternAgreement(source, fitted image.Image, target PageSize) (int, int) {
	scale := target.Width / 100
	offset := (target.Height - target.Width) / 2
	checked, wrong := 0, 0

	for y := 2; y < 98; y++ {
		for x := 2; x < 98; x++ {
			pixel := guardRGB(source, x, y)
			if pixel == ([3]uint8{255, 255, 255}) {
				continue
			}

			if pixel != guardRGB(source, x-1, y) || pixel != guardRGB(source, x+1, y) || pixel != guardRGB(source, x, y-1) ||
				pixel != guardRGB(source, x, y+1) {
				continue
			}

			fx := int((float64(x) + 0.5) * scale)
			fy := int(target.Height - offset - (100-float64(y)-0.5)*scale)
			checked++

			if guardRGB(fitted, fx, fy) != pixel {
				wrong++
			}
		}
	}

	return checked, wrong
}

func TestFitPatternSourceAndOutputRendering(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)

	renderers := []string{fitRendererPoppler}
	if pdforacle.HasMuPDF() {
		renderers = append(renderers, fitRendererMuPDF)
	}

	for _, paintType := range []int{1, 2} {
		for _, target := range []PageSize{{210 * 72 / 25.4, 297 * 72 / 25.4}, {612, 1008}} {
			t.Run(strconv.Itoa(paintType)+"/"+strconv.FormatFloat(target.Height, 'f', 0, 64), func(t *testing.T) {
				t.Parallel()
				guardPatternRenderCase(t, tools, paintType, target, renderers)
			})
		}
	}
}

func guardPatternRenderCase(t *testing.T, tools pdforacle.Tools, paintType int, target PageSize, renderers []string) {
	t.Helper()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, fitRenderSource)
	source := guardPatternDocument(t, paintType)
	guardWriteDocument(t, source, sourcePath)

	fitted := guardPatternDocument(t, paintType)
	if _, err := inspectPageFits(t.Context(), fitted, target); err != nil {
		t.Fatal(err)
	}

	fitStaticTestPages(t, fitted, target)

	fittedPath := filepath.Join(dir, fitRenderOutput)
	guardWriteDocument(t, fitted, fittedPath)

	if _, err := pdforacle.Load(tools, fittedPath); err != nil {
		t.Fatal(err)
	}

	for _, renderer := range renderers {
		t.Logf("independent renderer: %s", renderer)
		sourceImage := guardRender(t, tools, sourcePath, renderer)
		fittedImage := guardRender(t, tools, fittedPath, renderer)
		guardComparePattern(t, sourceImage, fittedImage, target)
	}
}
