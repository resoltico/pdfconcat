// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

const fitPaperLegal = "Legal"

func TestFittedFormIsolationSurvivesGraphicsStackEscapes(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)

	for _, paper := range []string{"A4", fitPaperLegal} {
		for _, prefix := range []string{"", "Q ", strings.Repeat("Q ", 10), strings.Repeat("q ", 2)} {
			t.Run(fmt.Sprintf("%s/%q", paper, prefix), func(t *testing.T) {
				t.Parallel()
				document := fitMarkedDocument(t, tools, paper, prefix)
				verifyFitSheet(t, document, paper)

				colors, renderErr := document.ColorCounts(1, [][3]uint8{{255, 0, 0}, {0, 0, 255}})
				if renderErr != nil || colors[0] != 0 || colors[1] < 10000 || colors[1] > 16000 {
					t.Fatalf("hidden/visible landmarks %v: %v", colors, renderErr)
				}
			})
		}
	}
}

func fitMarkedDocument(t *testing.T, tools pdforacle.Tools, paper, prefix string) *pdforacle.Document {
	t.Helper()

	pdf := readUnvalidated(t, pdffixture.FitMarks(prefix))

	size, err := assembly.ParsePageSize(paper)
	if err != nil {
		t.Fatal(err)
	}

	target := PageSize{float64(size.Dim.Width), float64(size.Dim.Height)}
	fitStaticTestPages(t, pdf, target)

	path := filepath.Join(t.TempDir(), "fitted.pdf")

	built := pool{pdf: pdf}
	if _, writeErr := built.write(t.Context(), path); writeErr != nil {
		t.Fatal(writeErr)
	}

	document, loadErr := pdforacle.Load(tools, path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}

	return document
}

func fitStaticTestPages(t *testing.T, pdf *model.Context, target PageSize) {
	t.Helper()

	root, err := pdf.Pages()
	if err != nil {
		t.Fatal(err)
	}

	err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		geometry, fitErr := decomposePage(t.Context(), pdf, page, inherited)
		if fitErr != nil {
			return fitErr
		}

		fit, fitErr := fitGeometry(geometry, target)
		if fitErr != nil {
			return fitErr
		}

		return fitPageContent(t.Context(), pdf, page, inherited, fit)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkFitResourceScope(t *testing.T, pdf *model.Context, page types.Dict, originalResources, originalGroup types.Object) {
	t.Helper()

	resources, err := pdf.DereferenceDict(page[keyResources])
	if err != nil {
		t.Fatal(err)
	}

	xobjects, err := pdf.DereferenceDict(resources[keyXObject])
	if err != nil {
		t.Fatal(err)
	}

	original, err := pdf.DereferenceDict(originalResources)
	if err != nil {
		t.Fatal(err)
	}

	originalXObjects, err := pdf.DereferenceDict(original[keyXObject])
	if err != nil {
		t.Fatal(err)
	}

	var wrapper types.Object

	for name, object := range xobjects {
		if originalXObjects[name] == nil {
			wrapper = object
		}
	}

	form, _, err := pdf.DereferenceStreamDict(wrapper)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(form.Dict[keyResources], originalResources) || form.Dict["Group"] != nil ||
		!reflect.DeepEqual(page["Group"], originalGroup) {
		t.Fatalf("original resource/group scope changed: %v", form.Dict)
	}

	if !reflect.DeepEqual(resources["ColorSpace"], original["ColorSpace"]) {
		t.Fatal("output page lost source color-space scope")
	}
}

func verifyFitSheet(t *testing.T, document *pdforacle.Document, paper string) {
	t.Helper()

	box := []float64{0, 0, 595.2755905511812, 841.8897637795276}
	landmark := [2]int{238, 480}

	if paper == fitPaperLegal {
		box = []float64{0, 0, 612, 1008}
		landmark = [2]int{245, 565}
	}

	expected := pdforacle.Expectation{Pages: []pdforacle.ExpectedPage{{
		Text:     "q Q /q /Q",
		Geometry: &pdforacle.Geometry{MediaBox: box, CropBox: box, UserUnit: 1},
	}}}
	if findings := document.Verify(expected); len(findings) != 0 {
		t.Fatalf("independent fitted sheet: %+v", findings)
	}

	color, err := document.PixelColor(1, landmark[0], landmark[1])
	if err != nil || color != [3]uint8{0, 0, 255} {
		t.Fatalf("independent blue landmark: %v: %v", color, err)
	}

	expected.Pages[0].Geometry.Rotate = 90
	if findings := pdforacle.Failed(document.Verify(expected)); len(findings) != 1 || findings[0] != pdforacle.CheckGeometry {
		t.Fatalf("wrong-rotation negative control did not fail: %v", findings)
	}
}

func TestFitContentPreservesGroupAndResourcesAndRefusesUnreadableContent(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, pdffixture.FitMarks(""))

	root, err := pdf.Pages()
	if err != nil {
		t.Fatal(err)
	}

	err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		group := types.Dict{"S": types.Name("Transparency"), "CS": types.Name("DeviceRGB"), "I": types.Boolean(true)}
		page["Group"] = group
		resources := inherited.override(page).resources

		geometry, fitErr := decomposePage(t.Context(), pdf, page, inherited)
		if fitErr != nil {
			return fitErr
		}

		fit, fitErr := fitGeometry(geometry, PageSize{612, 1008})
		if fitErr != nil {
			return fitErr
		}

		if contentErr := fitPageContent(t.Context(), pdf, page, inherited, fit); contentErr != nil {
			return contentErr
		}

		checkFitResourceScope(t, pdf, page, resources, group)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if fitErr = fitPageContent(ctx, pdf, page, inherited, fit); !errors.Is(fitErr, context.Canceled) {
			t.Fatalf("fit cancellation: %v", fitErr)
		}

		page["Contents"] = types.Integer(7)
		if fitErr = fitPageContent(t.Context(), pdf, page, inherited, fit); fitErr == nil {
			t.Fatal("unreadable content fitted")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
