// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestFiniteForwardTransformsDoNotRequireRepresentableDeterminants(t *testing.T) {
	for _, name := range []string{"finite-scale-det-underflow", "finite-scale-det-overflow"} {
		t.Run(name, func(t *testing.T) {
			source := filepath.Join("testdata/numeric", name+".pdf")
			qpdfCheck(t, source)
			data := readPDF(t, source)
			for _, op := range []app.Operation{app.Check, app.Build} {
				r, _, output := selectiveRequest(t, data, "200x200", op)
				if r.Status != "ok" {
					t.Fatal(op, r.Diagnostics)
				}
				if op == app.Build {
					qpdfCheck(t, output)
				}
			}
		})
	}
}
func TestLargeIntegralRealKeepsPortablePDFSyntax(t *testing.T) {
	source := filepath.Join("testdata/numeric", "large-real-unit.pdf")
	qpdfCheck(t, source)
	r, _, output := selectiveRequest(t, readPDF(t, source), "", app.Build)
	if r.Status != "ok" || !r.Output.Published {
		t.Fatal(r.Diagnostics)
	}
	qpdfCheck(t, output)
	before, after := qpdfPages(t, source), qpdfPages(t, output)
	if before[0]["/UserUnit"] != after[0]["/UserUnit"] {
		t.Fatal("real value changed", before, after)
	}
}
func TestActualNonfiniteScaleStillRefuses(t *testing.T) {
	source := filepath.Join("testdata/numeric", "genuinely-nonfinite-scale.pdf")
	qpdfCheck(t, source)
	for _, op := range []app.Operation{app.Check, app.Build} {
		r, _, _ := selectiveRequest(t, readPDF(t, source), "14400x14400", op)
		if r.Status != "invalid" || !codePresent(r.Diagnostics, diag.FitGeometryUnsupported) || r.Output.Published {
			t.Fatal(r.Diagnostics)
		}
	}
}
