// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"testing"
)

const (
	sourceNullBoxPage = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources <<>> /TrimBox 4 0 R >>"
	sourceNullBoxTree = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
)

func TestSourceIndirectNullPrintBoxRemainsAbsent(t *testing.T) {
	t.Parallel()

	data := sourceIdentityPDF(fitCatalogSourceVersion, "", []string{catalog, sourceNullBoxTree, sourceNullBoxPage, nullPDFObject}, nil, nil)
	engine, path := fitBoundaryRawSource(t, data)

	for _, target := range []PageSize{{595, 842}, {612, 1008}} {
		info, err := engine.Inspect(t.Context(), path, &target)
		if err != nil {
			t.Fatal(err)
		}

		if info.Pages != 1 || info.First != (PageSize{100, 100}) || len(info.Fits) != 1 {
			t.Fatalf("null optional print box changed captured geometry: %+v", info)
		}
	}
}

func TestSourceNullPageTreeNodeIsRefused(t *testing.T) {
	t.Parallel()

	data := sourceIdentityPDF(fitCatalogSourceVersion, "", []string{catalog, sourceNullBoxTree, nullPDFObject}, nil, nil)
	engine, path := fitBoundaryRawSource(t, data)

	for _, target := range []*PageSize{nil, {595, 842}, {612, 1008}} {
		_, err := engine.Inspect(t.Context(), path, target)

		var engineErr *Error
		if !errors.As(err, &engineErr) || engineErr.Code != CodeInvalid {
			t.Fatalf("null required page-tree node accepted or misclassified: %v", err)
		}
	}
}
