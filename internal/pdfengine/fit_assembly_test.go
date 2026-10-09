// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestFittedAssemblyUsesOnePoolAndPreservesOriginalClipping(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)
	for _, paper := range []string{"A4", "Legal"} {
		t.Run(paper, func(t *testing.T) {
			t.Parallel()
			document, info, digest := assembleFittedMarks(t, tools, paper)

			for page := 1; page <= 2; page++ {
				colors, renderErr := document.ColorCounts(page, [][3]uint8{{255, 0, 0}, {0, 0, 255}})
				if renderErr != nil || colors[0] != 0 || colors[1] < 10000 {
					t.Fatalf("fitted assembly clipping/landmarks %v: %v", colors, renderErr)
				}
			}

			if len(info.Fits) != 1 || info.Fits[0].Last != 1 || info.Fits[0].Fit.Target.Width <= 0 || digest == "" {
				t.Fatalf("captured/verified fit facts: %+v", info)
			}
		})
	}
}

func assembleFittedMarks(t *testing.T, tools pdforacle.Tools, paper string) (*pdforacle.Document, pdfengine.SourceInfo, string) {
	t.Helper()
	dir := t.TempDir()

	path := filepath.Join(dir, "marks.pdf")
	if err := pdffixture.FitMarks(strings.Repeat("Q ", 10)).WriteFile(path); err != nil {
		t.Fatal(err)
	}

	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}

	size, err := assembly.ParsePageSize(paper)
	if err != nil {
		t.Fatal(err)
	}

	target := pdfengine.PageSize{Width: float64(size.Dim.Width), Height: float64(size.Dim.Height)}

	info, err := engine.Inspect(t.Context(), path, &target)
	if err != nil {
		t.Fatal(err)
	}

	request := pdfengine.AssembleRequest{
		FitTarget:     &target,
		Sources:       []pdfengine.SourceFile{{Path: path, Info: info}},
		Order:         []pdfengine.Run{pdfengine.SourcePages(0, 1, 1), pdfengine.SourcePages(0, 1, 1)},
		ExpectedPages: 2, Destination: filepath.Join(dir, "output.pdf"),
	}
	if err = engine.Assemble(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	document, err := pdforacle.Load(tools, request.Destination)
	if err != nil {
		t.Fatal(err)
	}

	return document, info, request.OutputDigest
}

func TestFittedInspectionAndDefensiveImportRejectRelativeLinksWithoutTouchingOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	path := filepath.Join(dir, "relative.pdf")
	if err := pdffixture.URILink("relative", "annex.pdf", "https://example.invalid/", false).WriteFile(path); err != nil {
		t.Fatal(err)
	}

	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}

	target := pdfengine.PageSize{Width: 612, Height: 1008}
	if _, err = engine.Inspect(t.Context(), path, &target); pdfengine.CodeOf(err) != pdfengine.CodeFitUnsupported {
		t.Fatalf("shared fit inspection accepted relative target: %v", err)
	}

	info, err := engine.Inspect(t.Context(), path, nil)
	if err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(dir, "output.pdf")
	if err = os.WriteFile(output, []byte("keep seeded target"), 0o600); err != nil {
		t.Fatal(err)
	}

	fit, err := pdfengine.CanvasFit(pdfengine.PageSize{Width: 612, Height: 792}, target)
	if err != nil {
		t.Fatal(err)
	}

	info.Fits = []pdfengine.FitRange{{First: 1, Last: 1, Fit: fit}} // Forged facts exercise the real defensive read.

	request := pdfengine.AssembleRequest{
		FitTarget: &target, Sources: []pdfengine.SourceFile{{Path: path, Info: info}},
		Order: []pdfengine.Run{pdfengine.SourcePages(0, 1, 1)}, ExpectedPages: 1, Destination: output,
	}
	if err = engine.Assemble(t.Context(), &request); pdfengine.CodeOf(err) != pdfengine.CodeFitUnsupported {
		t.Fatalf("defensive import accepted relative target: %v", err)
	}

	bytes, err := os.ReadFile(filepath.Clean(output))
	if err != nil || string(bytes) != "keep seeded target" {
		t.Fatalf("fit refusal changed destination: %q %v", bytes, err)
	}
}
