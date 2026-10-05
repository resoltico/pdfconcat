// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package pdffixture creates small synthetic PDFs for tests.
package pdffixture

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

// A4 returns the portrait A4 page size in points.
func A4() assembly.PageDim {
	const widthPoints, heightPoints = 595, 842

	return assembly.PageDim{Width: widthPoints, Height: heightPoints}
}

// Write creates a valid PDF at path with the given number of pages of size dim, each labelled with its number.
func Write(tb testing.TB, path string, pages int, dim assembly.PageDim) {
	tb.Helper()

	engine, err := pdfengine.NewPDFCPU()
	if err != nil {
		tb.Fatalf("create PDF engine: %v", err)
	}

	layout := assembly.Layout{TotalPages: pages}

	for page := 1; page <= pages; page++ {
		label := fmt.Sprintf("%s page %d", filepath.Base(path), page)

		spec, resolveErr := assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Some(label)}}.Resolve(dim)
		if resolveErr != nil {
			tb.Fatalf("resolve fixture page: %v", resolveErr)
		}

		layout.Parts = append(layout.Parts, assembly.Part{Kind: assembly.Blank, Blank: spec, Pages: 1, FirstPage: page})
	}

	err = engine.Assemble(context.Background(), layout, tb.TempDir(), path)
	if err != nil {
		tb.Fatalf("write fixture %s: %v", path, err)
	}
}
