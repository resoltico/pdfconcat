// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestNullPageKidsPreserveIndependentPaintAcrossAllModes(t *testing.T) {
	t.Parallel()

	for _, paper := range []assembly.FitTarget{"", assembly.FitA4, assembly.FitLegal} {
		for _, value := range []string{"absent", pageKidsNullLiteral, "6 0 R"} {
			doc := pdffixture.Plain(pageKidsParentMarker)
			if value != "absent" {
				doc = pageKidsDocument(value, pageKidsNullLiteral)
			}

			checkSafePageKidsOutput(t, paper, doc)
		}
	}
}

func checkSafePageKidsOutput(t *testing.T, paper assembly.FitTarget, doc *pdffixture.Doc) {
	t.Helper()

	tools := pdforacle.RequireTools(t)

	source := filepath.Join(t.TempDir(), "safe-page.pdf")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}

	engine := newEngine(t)

	var target *pdfengine.PageSize

	if paper != "" {
		resolved := fitPrintTarget(t, paper)
		target = &resolved
	}

	info, err := engine.Inspect(t.Context(), source, target)
	if err != nil || info.Pages != 1 || target != nil && (len(info.Fits) != 1 || info.Fits[0].Last != 1) {
		t.Fatalf("safe typed leaf lost its actual page: %+v %v", info, err)
	}

	request := fitPrintRequest(source, target, info)
	if err = engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	verifyPageKidsPaint(t, tools, request.Destination)
}

func verifyPageKidsPaint(t *testing.T, tools pdforacle.Tools, path string) {
	t.Helper()

	output, err := pdforacle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}

	findings := output.Verify(pdforacle.Expectation{Pages: []pdforacle.ExpectedPage{{Text: pageKidsParentMarker}}})
	if len(findings) != 0 {
		t.Fatalf("safe leaf source content lost: %v", findings)
	}

	colors, renderErr := output.ColorCounts(1, [][3]uint8{{0, 0, 0}})
	if renderErr != nil || colors[0] < 100 {
		t.Fatalf("safe leaf paint lost: %v %v", colors, renderErr)
	}
}
