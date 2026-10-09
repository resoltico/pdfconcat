// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestIndependentReaderDetectsNativeAcceptedPageKidsHazard(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)
	pdf := readUnvalidated(t, pdffixture.Plain("native accepted parent"))

	page, err := pdf.DereferenceDictContext(t.Context(), *types.NewIndirectRef(3, 0))
	if err != nil {
		t.Fatal(err)
	}

	page[keyKids] = types.Array{}

	if err = api.ValidateContext(t.Context(), pdf); err != nil {
		t.Fatalf("nativecontrolfailedbeforeknownreaderhazard: %v", err)
	}
	// Deliberately bypass the application's raw admissibility guard to retain the
	// original accepted-native writer shape as a real independent negative control.
	output := filepath.Join(t.TempDir(), "retained-page-kids.pdf")
	guardWriteDocument(t, pdf, output)

	document, loadErr := pdforacle.Load(tools, output)
	if loadErr == nil {
		findings := document.Verify(pdforacle.Expectation{Pages: []pdforacle.ExpectedPage{{Text: "native accepted parent"}}})
		if len(findings) == 0 {
			t.Fatal("independent reader failed to detect retained nonnull leaf Kids")
		}
	}
}
