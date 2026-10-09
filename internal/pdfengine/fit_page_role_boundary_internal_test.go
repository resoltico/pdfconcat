// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestTypedPageLeafKeepsContentDespitePassiveKidsEntry(t *testing.T) {
	t.Parallel()

	for _, extra := range []string{"", " /Kids null"} {
		checkTypedPageNativeKids(t, extra)
	}
}

func checkTypedPageNativeKids(t *testing.T, extra string) {
	t.Helper()

	doc := pdffixture.Plain("typed page role")
	doc.Objs[2] = []byte(strings.TrimSuffix(string(doc.Objs[2]), ">>") + extra + " >>")

	source := filepath.Join(t.TempDir(), "typed-page.pdf")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range []*PageSize{nil, {612, 1008}} {
		checkTypedPageNativeTarget(t, engine, source, target)
	}
}

func checkTypedPageNativeTarget(t *testing.T, engine *Engine, source string, target *PageSize) {
	t.Helper()

	info, inspectErr := engine.Inspect(t.Context(), source, target)
	if inspectErr != nil || info.Pages != 1 || target != nil && (len(info.Fits) != 1 || info.Fits[0].Last != 1) {
		t.Fatalf("typed leaf lost its actual page/content: %+v %v", info, inspectErr)
	}

	importer := pool{engine: engine}

	pdf, importErr := importer.readImport(t.Context(), 0, source, target)
	if importErr != nil {
		t.Fatal(importErr)
	}

	if target != nil {
		if err := verifyFittedSheets(t.Context(), pdf, *target); err != nil {
			t.Fatal(err)
		}
	}
}
