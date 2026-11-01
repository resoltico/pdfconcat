// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func TestEmergencyWrappingPreservesDecomposedTextAndRenderedMarks(t *testing.T) {
	t.Parallel()
	font := defaultFont(t)
	assertEmergencyReference(t, font, "x\u0301x\u0301x\u0304x\u0304x\u0327x\u0327", "x\u0301x\u0301\nx\u0304x\u0304\nx\u0327x\u0327")
}

func TestEmergencyWrappingWithExternalFont(t *testing.T) {
	t.Parallel()
	font, err := typeset.LoadFont(gomono.TTF)
	if err != nil {
		t.Fatal(err)
	}
	assertEmergencyReference(t, font, "AVfi1234", "AV\nfi\n12\n34")
}

func assertEmergencyReference(t *testing.T, font *typeset.Font, text, expected string) {
	t.Helper()
	spec := params(text)
	spec.WrapWidth, spec.Wrap = 30, typeset.WrapEmergency
	emergency := placed(t, font, spec)
	// These known glyphs fit two per line. The authored reference is independent
	// of the wrapper's chosen line texts and preserves source characters.
	spec.Text, spec.Wrap = expected, typeset.WrapWord
	authored := placed(t, font, spec)
	page := func(block *typeset.Placed) compose.Page {
		return compose.Page{Width: 300, Height: 200, Blocks: []compose.TextBlock{{Text: block}}}
	}
	actualPath, _ := writePDF(t, []compose.Page{page(emergency)})
	referencePath, _ := writePDF(t, []compose.Page{page(authored)})
	if count := qpdfCheck(t, actualPath); count != 1 {
		t.Fatalf("page count: %d", count)
	}
	if got := strings.TrimRight(extractRaw(t, actualPath)[0], "\n"); got != expected {
		t.Fatalf("wrapped extraction: %q, want %q", got, expected)
	}
	actual, reference := renderGray(t, actualPath), renderGray(t, referencePath)
	if _, found := actual.ink(128); !found {
		t.Fatal("wrapped text rendered without ink")
	}
	if diff := actual.sumAbsDiff(reference); diff != 0 {
		t.Fatalf("wrapped glyphs differ from authored reference by %d", diff)
	}
}
