// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"fmt"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestFittedPagePreservesSingleBlendingContextAndResourceScope(t *testing.T) {
	t.Parallel()

	for _, isolation := range []types.Object{nil, types.Boolean(false), types.Boolean(true)} {
		pdf := readUnvalidated(t, pdffixture.FitMarks(""))

		root, err := pdf.Pages()
		if err != nil {
			t.Fatal(err)
		}

		err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
			return assertFitBlendingContext(t, pdf, page, inherited, isolation)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertFitBlendingContext(t *testing.T, pdf *model.Context, page types.Dict, inherited inheritedAttrs, isolation types.Object) error {
	t.Helper()

	group := types.Dict{"S": types.Name("Transparency"), "CS": types.Name(fitDeviceRGB), "K": types.Boolean(true)}
	if isolation != nil {
		group["I"] = isolation
	}

	colors := types.Dict{"DefaultRGB": types.Name(fitDeviceRGB), "SourceColor": types.Name(fitDeviceCMYK)}
	original := types.Dict{fitColorSpace: colors, keyXObject: types.Dict{fitContentName: types.Name("source-scope")}}
	page[keyResources], page["Group"] = original, group

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

	checkFitResourceScope(t, pdf, page, original, group)

	resources, readErr := pdf.DereferenceDict(page[keyResources])
	if readErr != nil {
		return fmt.Errorf("inspect fitted resource scope: %w", readErr)
	}

	resources["private-wrapper"] = types.Boolean(true)

	if original["private-wrapper"] != nil || group["I"] != isolation || group["K"] != types.Boolean(true) {
		t.Fatal("fitting mutated shared original context")
	}

	return nil
}
