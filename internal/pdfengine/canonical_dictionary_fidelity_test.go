// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

const (
	canonicalGenuineCrop = "genuine"
	canonicalPrivateLeaf = "private leaf"
	canonicalPlainCrop   = "plain"
	canonicalPDFVersion  = "1.7"
)

func canonicalCropSource(location string) *pdffixture.Doc {
	pageExtra, treeExtra := "", ""

	switch location {
	case canonicalGenuineCrop:
		pageExtra = " /Cr#6fpBox [20 20 80 80]"
	case canonicalPrivateLeaf:
		pageExtra = " /Cr#236fpBox [20 20 80 80]"
	case "private inherited":
		treeExtra = " /Cr#236fpBox [20 20 80 80]"
	case canonicalPlainCrop:
	default:
		panic("unknown canonical crop fixture")
	}

	paint := "1 0 0 rg 5 5 8 8 re f 87 87 8 8 re f 0 0 1 rg 40 40 20 20 re f\n"

	return &pdffixture.Doc{Version: canonicalPDFVersion, Objs: [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 100 100]" + treeExtra + " >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /Resources << >> /Contents 4 0 R" + pageExtra + " >>"),
		[]byte(fmt.Sprintf(fitOracleStreamFormat, len(paint), paint)),
	}}
}

func TestCanonicalPrivateAndEscapedCropNamesPreserveIndependentFidelity(t *testing.T) {
	t.Parallel()

	for _, mode := range []assembly.FitTarget{"", assembly.FitA4, assembly.FitLegal} {
		for _, location := range []string{canonicalPlainCrop, canonicalGenuineCrop, canonicalPrivateLeaf, "private inherited"} {
			checkCanonicalCropFidelity(t, mode, location)
		}
	}
}

func checkCanonicalCropFidelity(t *testing.T, mode assembly.FitTarget, location string) {
	t.Helper()
	tools := pdforacle.RequireTools(t)

	source := filepath.Join(t.TempDir(), "canonical-crop.pdf")
	if err := canonicalCropSource(location).WriteFile(source); err != nil {
		t.Fatal(err)
	}

	engine := newEngine(t)

	var target *pdfengine.PageSize

	if mode != "" {
		resolved := fitPrintTarget(t, mode)
		target = &resolved
	}

	info, err := engine.Inspect(t.Context(), source, target)
	if err != nil {
		t.Fatal(err)
	}

	checkCanonicalCapturedGeometry(t, info, target, location)

	request := fitPrintRequest(source, target, info)
	if err = engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	dump := loadFitLinkDump(t, request.Destination)

	page := dump.dictionary(t, dump.pages[0])
	if target != nil {
		checkCanonicalSourceFormBox(t, dump, page, location)
	}

	if location == canonicalPrivateLeaf && page["/Cr#6fpBox"] == nil {
		t.Fatal("canonical private identity lost on import")
	}

	if target == nil && location != canonicalGenuineCrop && page["/CropBox"] != nil {
		t.Fatal("ordinary import manufactured CropBox from private name")
	}

	checkCanonicalCropPaint(t, tools, request.Destination, target, location)
}

func checkCanonicalCropPaint(t *testing.T, tools pdforacle.Tools, path string, target *pdfengine.PageSize, location string) {
	t.Helper()

	output, err := pdforacle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}

	colors, err := output.ColorCounts(1, [][3]uint8{{255, 0, 0}, {0, 0, 255}})
	if err != nil {
		t.Fatal(err)
	}

	if colors[1] == 0 {
		t.Fatal("source central blue paint lost")
	}

	if location == canonicalGenuineCrop {
		if colors[0] != 0 {
			t.Fatalf("genuine escaped CropBox failed to clip red corners: %v", colors)
		}

		return
	}

	scale := 1.0
	if target != nil {
		scale = math.Min(target.Width/100, target.Height/100)
	}

	wantRed := 128 * scale * scale
	if float64(colors[0]) < wantRed*0.85 || float64(colors[0]) > wantRed*1.15 {
		t.Fatalf("canonical private scope clipped/scaled source marks: %s red%d want~%g", location, colors[0], wantRed)
	}
}

func checkCanonicalSourceFormBox(t *testing.T, dump fitLinkDump, page map[string]any, location string) {
	t.Helper()
	resources := dump.dictionary(t, page["/Resources"])
	xobjects := dump.dictionary(t, resources["/XObject"])

	ref, present := xobjects["/FittedSource"].(string)
	if !present {
		t.Fatal("source Form binding missing")
	}

	record, present := dump.objects["obj:"+ref].(map[string]any)
	if !present {
		t.Fatal("serialized source Form object missing")
	}

	stream, present := record["stream"].(map[string]any)
	if !present {
		t.Fatal("source Form stream missing")
	}

	dict, present := stream["dict"].(map[string]any)
	if !present {
		t.Fatal("source Form dictionary missing")
	}

	bbox := fitOracleNumbers(t, dict["/BBox"])

	want := [4]float64{0, 0, 100, 100}
	if location == canonicalGenuineCrop {
		want = [4]float64{20, 20, 80, 80}
	}

	for index, value := range want {
		if bbox[index] != value {
			t.Fatalf("private name changed source clipping box: %v want %v", bbox, want)
		}
	}
}

func checkCanonicalCapturedGeometry(t *testing.T, info pdfengine.SourceInfo, target *pdfengine.PageSize, location string) {
	t.Helper()

	extent := 100.0
	visible := [4]float64{0, 0, 100, 100}

	if location == canonicalGenuineCrop {
		extent = 60
		visible = [4]float64{20, 20, 80, 80}
	}

	if info.First != (pdfengine.PageSize{Width: extent, Height: extent}) {
		t.Fatalf("private name became source geometry: %s %+v", location, info)
	}

	if target != nil && (len(info.Fits) != 1 || info.Fits[0].Fit.Visible != visible) {
		t.Fatalf("captured fit disagrees with independent source visibility: %s %+v", location, info.Fits)
	}
}
