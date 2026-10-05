// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

func newEngine(t *testing.T) *pdfengine.PDFCPU {
	t.Helper()

	engine, err := pdfengine.NewPDFCPU()
	if err != nil {
		t.Fatalf("NewPDFCPU() error = %v", err)
	}

	return engine
}

func blankLayout(t *testing.T, style assembly.BlankStyle, dim assembly.PageDim, count int) assembly.Layout {
	t.Helper()

	spec, err := style.Resolve(dim)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	return assembly.Layout{
		Parts:      []assembly.Part{{Kind: assembly.Blank, Blank: spec, Pages: count, FirstPage: 1}},
		BlankPages: count,
		TotalPages: count,
	}
}

func TestAssembleRendersValidatedBlankPages(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)
	dim := assembly.PageDim{Width: 595, Height: 842}
	style := assembly.BlankStyle{
		Background: assembly.Some(assembly.Color{R: 0xEE, G: 0xEE, B: 0xEE}),
		Text: assembly.TextStyle{
			Value: assembly.Some("Intentionally left blank (café – “quoted” \\ back)"),
			Font:  assembly.Some(assembly.Font("Times-Italic")),
		},
	}

	destination := filepath.Join(t.TempDir(), "out.pdf")
	if err := engine.Assemble(context.Background(), blankLayout(t, style, dim, 3), t.TempDir(), destination); err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}

	info, err := engine.Inspect(context.Background(), destination)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if info.Pages != 3 || info.FirstPage != dim || info.LastPage != dim {
		t.Fatalf("Inspect() = %+v, want 3 pages of %+v", info, dim)
	}
}

func TestValidateBlankRejectsCharactersOutsideStandardFontRepertoire(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)

	spec, err := assembly.BlankStyle{Text: assembly.TextStyle{Value: assembly.Some("Ā")}}.Resolve(assembly.PageDim{Width: 595, Height: 842})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	err = engine.ValidateBlank(context.Background(), spec)
	if err == nil {
		t.Fatal("ValidateBlank() accepted a character outside Windows-1252")
	}
}

func TestInspectRejectsNonRegularAndMissingFiles(t *testing.T) {
	t.Parallel()

	engine := newEngine(t)
	for _, path := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing.pdf")} {
		if _, err := engine.Inspect(context.Background(), path); err == nil {
			t.Errorf("Inspect(%q) succeeded", path)
		}
	}
}
