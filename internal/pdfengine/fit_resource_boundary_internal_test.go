// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitResourceInspectionAndDefensiveImportShareRefusal(t *testing.T) {
	t.Parallel()

	for _, target := range []PageSize{{595.28, 841.89}, {612, 1008}} {
		t.Run(targetName(target), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			page := guardPage(t, pdf)
			scope := types.Dict{}
			self := guardStream(t, pdf, guardSelfDo, scope)
			scope[keyXObject] = types.Dict{"Self": self}
			page[keyResources] = scope
			content := guardStream(t, pdf, guardSelfDo, nil)
			page[keyContents] = content
			source := filepath.Join(t.TempDir(), "recursive.pdf")
			guardWriteDocument(t, pdf, source)
			checkFitResourceBoundary(t, source, target)
		})
	}
}

func checkFitResourceBoundary(t *testing.T, source string, target PageSize) {
	t.Helper()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	_, err = engine.Inspect(t.Context(), source, &target)
	assertFitResourceCycleRefusal(t, err)

	info, err := engine.Inspect(t.Context(), source, nil)
	if err != nil {
		t.Fatal(err)
	}

	fit, err := CanvasFit(PageSize{612, 792}, target)
	if err != nil {
		t.Fatal(err)
	}

	info.Fits = []FitRange{{First: 1, Last: 1, Fit: fit}}
	destination := filepath.Join(t.TempDir(), "seed.pdf")

	const seed = "preserve seeded destination"
	if err = os.WriteFile(destination, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	request := AssembleRequest{
		FitTarget: &target, Sources: []SourceFile{{Path: source, Info: info}},
		Order: []Run{SourcePages(0, 1, 1)}, ExpectedPages: 1, Destination: destination,
	}
	err = engine.Assemble(t.Context(), &request)
	assertFitResourceCycleRefusal(t, err)

	output, err := os.ReadFile(filepath.Clean(destination))
	if err != nil || string(output) != seed {
		t.Fatalf("resource refusal changed seeded destination: %q %v", output, err)
	}
}

func assertFitResourceCycleRefusal(t *testing.T, err error) {
	t.Helper()

	if CodeOf(err) != CodeFitUnsupported || !strings.Contains(err.Error(), "recurs") {
		t.Fatalf("expected shared executed-resource recursion refusal: %v", err)
	}
}

func targetName(target PageSize) string {
	if target.Width == 612 {
		return "Legal"
	}

	return "A4"
}
