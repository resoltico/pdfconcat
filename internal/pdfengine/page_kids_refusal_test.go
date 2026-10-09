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
)

const (
	pageKidsParentMarker = "parent page paint"
	pageKidsNullLiteral  = "null"
)

func pageKidsDocument(value string, objects ...string) *pdffixture.Doc {
	doc := pdffixture.Plain(pageKidsParentMarker)
	body := strings.Replace(string(doc.Objs[2]), "/Type /Page", "/Type /P#61ge", 1)

	doc.Objs[2] = []byte(strings.TrimSuffix(body, ">>") + " /K#69ds " + value + " >>")
	for _, object := range objects {
		doc.Objs = append(doc.Objs, []byte(object))
	}

	return doc
}

func TestPageKidsAmbiguityRefusesRawCheckAndForgedImportAcrossAllModes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value   string
		objects []string
	}{
		{value: "[]"},
		{value: "1"},
		{value: "[999 0 R]"},
		{value: "[3 0 R]"},
		{value: "[2 0 R]"},
		{value: "6 0 R", objects: []string{"[]"}},
		{value: "999 0 R"},
		{value: "[6 0 R]", objects: []string{"<< /Type /Page /Parent 3 0 R /MediaBox [0 0 612 792] >>"}},
	} {
		for _, paper := range []assembly.FitTarget{"", assembly.FitA4, assembly.FitLegal} {
			checkPageKidsRefusal(t, paper, pageKidsDocument(test.value, test.objects...))
		}
	}
}

func checkPageKidsRefusal(t *testing.T, paper assembly.FitTarget, doc *pdffixture.Doc) {
	t.Helper()
	engine := newEngine(t)

	var target *pdfengine.PageSize

	want := pdfengine.CodeInvalid

	if paper != "" {
		resolved := fitPrintTarget(t, paper)
		target = &resolved
		want = pdfengine.CodeFitUnsupported
	}

	source := filepath.Join(t.TempDir(), "ambiguous-page.pdf")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}

	_, err := engine.Inspect(t.Context(), source, target)
	if pdfengine.CodeOf(err) != want || !strings.Contains(err.Error(), "source page 1 (object 3 0 R)") ||
		!strings.Contains(err.Error(), "recheck") {
		t.Fatalf("located raw refusal=%v want=%s", err, want)
	}

	checkPageKidsForgedImport(t, engine, source, target, want)
}

func checkPageKidsForgedImport(t *testing.T, engine *pdfengine.Engine, source string, target *pdfengine.PageSize, want pdfengine.Code) {
	t.Helper()

	plain := filepath.Join(t.TempDir(), "plain.pdf")
	if err := pdffixture.Plain("forged facts").WriteFile(plain); err != nil {
		t.Fatal(err)
	}

	forged, err := engine.Inspect(t.Context(), plain, target)
	if err != nil {
		t.Fatal(err)
	}

	request := fitPrintRequest(source, target, forged)

	const seed = "existing destination before page-role refusal"
	if err = os.WriteFile(request.Destination, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	err = engine.Assemble(t.Context(), &request)
	if pdfengine.CodeOf(err) != want || !strings.Contains(err.Error(), "source page 1 (object 3 0 R)") {
		t.Fatalf("forged import bypassed raw source refusal: %v", err)
	}

	data, readErr := os.ReadFile(filepath.Clean(request.Destination))
	if readErr != nil || string(data) != seed || request.OutputDigest != "" {
		t.Fatalf("refusal changed/published destination: %q %v", data, readErr)
	}
}
