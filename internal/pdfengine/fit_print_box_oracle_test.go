// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func fitPrintBoxSource(extra string) *pdffixture.Doc {
	doc := pdffixture.Plain("print boxes")
	doc.Objs[2] = []byte(strings.TrimSuffix(string(doc.Objs[2]), ">>") + extra + " >>")

	return doc
}

func fitPrintTarget(t *testing.T, paper assembly.FitTarget) pdfengine.PageSize {
	t.Helper()

	dim, err := paper.Dim()
	if err != nil {
		t.Fatal(err)
	}

	return pdfengine.PageSize{Width: float64(dim.Width), Height: float64(dim.Height)}
}

func fitPrintRequest(source string, target *pdfengine.PageSize, info pdfengine.SourceInfo) pdfengine.AssembleRequest {
	return pdfengine.AssembleRequest{
		FitTarget: target, Sources: []pdfengine.SourceFile{{Path: source, Info: info}},
		Order: []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)}, ExpectedPages: 1,
		Destination: filepath.Join(filepath.Dir(source), "fitted-print-boxes.pdf"),
	}
}

func TestFittedPrintBoxesPreserveIndependentSerializedGeometryOnBothPapers(t *testing.T) {
	t.Parallel()

	for _, paper := range []assembly.FitTarget{assembly.FitA4, assembly.FitLegal} {
		t.Run(string(paper), func(t *testing.T) {
			t.Parallel()
			verifyIndependentPrintBoxes(t, paper)
		})
	}
}

func verifyIndependentPrintBoxes(t *testing.T, paper assembly.FitTarget) {
	t.Helper()

	tools := pdforacle.RequireTools(t)
	source := filepath.Join(t.TempDir(), "print-boxes.pdf")

	doc := fitPrintBoxSource(" /BleedBox [20 30 580 750] /TrimBox [30 40 570 740] /ArtBox [40 50 560 730]")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}

	engine := newEngine(t)
	target := fitPrintTarget(t, paper)

	info, err := engine.Inspect(t.Context(), source, &target)
	if err != nil {
		t.Fatal(err)
	}

	request := fitPrintRequest(source, &target, info)
	if err = engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	dump := loadFitLinkDump(t, request.Destination)
	checkSerializedPrintCoordinates(t, dump.dictionary(t, dump.pages[0]), target)

	rendered, err := pdforacle.Load(tools, request.Destination)
	if err != nil {
		t.Fatal(err)
	}

	findings := rendered.Verify(pdforacle.Expectation{Pages: []pdforacle.ExpectedPage{{Text: "print boxes"}}})
	if len(findings) != 0 {
		t.Fatalf("fitted print-box content lost: %+v", findings)
	}

	colors, renderErr := rendered.ColorCounts(1, [][3]uint8{{0, 0, 0}})
	if renderErr != nil || colors[0] < 100 {
		t.Fatalf("fitted print-box page lost visible source paint: %v %v", colors, renderErr)
	}
}

func checkSerializedPrintCoordinates(t *testing.T, page map[string]any, target pdfengine.PageSize) {
	t.Helper()

	scale := math.Min(target.Width/612, target.Height/792)
	dx, dy := (target.Width-612*scale)/2, (target.Height-792*scale)/2
	originalBoxes := map[string][4]float64{
		"/BleedBox": {20, 30, 580, 750}, "/TrimBox": {30, 40, 570, 740}, "/ArtBox": {40, 50, 560, 730},
	}

	for key, original := range originalBoxes {
		actual := fitOracleNumbers(t, page[key])

		for index, sourceValue := range original {
			offset := dx
			if index%2 != 0 {
				offset = dy
			}

			want := scale*sourceValue + offset
			if math.Abs(actual[index]-want) > 1e-7 {
				t.Fatalf("independent %s edge%d mapping=%g want=%g", key, index, actual[index], want)
			}
		}
	}
}

func TestFitPrintBoxRawRefusalMatchesDefensiveImportAndPreservesDestination(t *testing.T) {
	t.Parallel()

	for _, paper := range []assembly.FitTarget{assembly.FitA4, assembly.FitLegal} {
		for _, extra := range []string{
			" /BleedBox [-1 0 30 30]", " /CropBox [100 100 500 700] /BleedBox [0 0 612 792]",
			" /BleedBox [0 0 20 20] /TrimBox [0 0 30 30]",
		} {
			checkPrintBoxRefusal(t, paper, extra)
		}
	}
}

func checkPrintBoxRefusal(t *testing.T, paper assembly.FitTarget, extra string) {
	t.Helper()

	source := filepath.Join(t.TempDir(), "source.pdf")
	if err := fitPrintBoxSource(extra).WriteFile(source); err != nil {
		t.Fatal(err)
	}

	engine := newEngine(t)
	target := fitPrintTarget(t, paper)

	_, err := engine.Inspect(t.Context(), source, &target)
	if pdfengine.CodeOf(err) != pdfengine.CodeFitUnsupported {
		t.Fatalf("raw print-box preflight failed to refuse: %v", err)
	}

	info, err := engine.Inspect(t.Context(), source, nil)
	if err != nil {
		t.Fatal(err)
	}

	request := fitPrintRequest(source, &target, info)

	const seed = "previous destination"

	if err = os.WriteFile(request.Destination, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	err = engine.Assemble(t.Context(), &request)
	if pdfengine.CodeOf(err) != pdfengine.CodeFitUnsupported {
		t.Fatalf("defensive print-box import admitted source: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Clean(request.Destination))
	if readErr != nil || string(data) != seed {
		t.Fatalf("print refusal changed destination: %q %v", data, readErr)
	}
}
