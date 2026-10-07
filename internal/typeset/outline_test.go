// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset_test

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

// glyphOf returns the glyph the default font uses for a single character.
func glyphOf(tb testing.TB, char string) uint16 {
	tb.Helper()

	var shaper typeset.Shaper

	placed, err := shaper.Place(defaultFont(tb), baseParams(char))
	if err != nil {
		tb.Fatal(err)
	}

	return placed.Lines[0].Clusters[0].Glyphs[0].ID
}

// fontWithCorruptOutline returns the default font whose glyf entry of glyph claims more contours than
// its data holds. The font still passes table validation: only reading the outline fails.
func fontWithCorruptOutline(tb testing.TB, glyph uint16) []byte {
	tb.Helper()

	return mutatedFont(tb, func(tables map[string][]byte) {
		const (
			longLocaFormat    = 1
			entryBytes        = 4
			headLocaFormat    = 50
			corruptedContours = 0x7FFF
		)

		if binary.BigEndian.Uint16(tables[headTableTag][headLocaFormat:]) != longLocaFormat {
			tb.Fatal("the default font is expected to use long loca offsets")
		}

		start := binary.BigEndian.Uint32(tables["loca"][entryBytes*int(glyph):])
		binary.BigEndian.PutUint16(tables["glyf"][start:], corruptedContours)
	})
}

func TestCorruptOutlineOfUsedGlyphIsReported(t *testing.T) {
	t.Parallel()

	glyph := glyphOf(t, "A")
	data := fontWithCorruptOutline(t, glyph)

	font, err := typeset.LoadFont(data)
	if err != nil {
		t.Fatalf("table validation must not read outlines: %v", err)
	}

	var shaper typeset.Shaper
	if validationErr := shaper.ValidateText(font, "A"); !errors.Is(validationErr, typeset.ErrMalformedFont) {
		t.Fatalf("static glyph outline validation: %v", validationErr)
	}

	placed, err := shaper.Place(font, baseParams("A"))

	var outline *typeset.OutlineError
	if !errors.As(err, &outline) || placed != nil {
		t.Fatalf(gotValueFormat, err)
	}

	if outline.Rune != 'A' || outline.Glyph != glyph {
		t.Errorf("error names U+%04X glyph %d, want U+0041 glyph %d", outline.Rune, outline.Glyph, glyph)
	}

	if outline.Font != font.PostScriptName() || !errors.Is(err, typeset.ErrMalformedFont) {
		t.Errorf("error does not identify the malformed font: %+v", outline)
	}

	for _, want := range []string{"NotoSans-Regular", "glyph", "U+0041"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
}

func TestStaticTextValidationRejectsAbsentFontAndGlyph(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper
	if err := shaper.ValidateText(nil, "A"); err == nil {
		t.Fatal("nil font accepted by static text validation")
	}

	font := defaultFont(t)
	if err := shaper.ValidateText(font, "\U0001F600"); err == nil {
		t.Fatal("missing glyph accepted before geometry")
	}
}

func TestStaticValidationAllowsShapingCompositionWithoutNominalMark(t *testing.T) {
	t.Parallel()
	base, composed := glyphOf(t, "a"), glyphOf(t, "ā")

	font, err := fontWithCmap(t, cmapOf(t, [3]uint32{'a', 'a', uint32(base)}, [3]uint32{'ā', 'ā', uint32(composed)}))
	if err != nil {
		t.Fatal(err)
	}

	var shaper typeset.Shaper
	if validationErr := shaper.ValidateText(font, "a\u0304"); validationErr != nil {
		t.Fatalf("valid font composition rejected before geometry: %v", validationErr)
	}

	if _, placementErr := shaper.Place(font, baseParams("a\u0304")); placementErr != nil {
		t.Fatalf("valid composition failed during placement: %v", placementErr)
	}
}

func TestIntactOutlinesPassOnRepeatedPlacement(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	font := defaultFont(t)

	for range 3 {
		_, err := shaper.Place(font, baseParams(strings.Repeat("Rīgas ļoti ", 50)))
		if err != nil {
			t.Fatal(err)
		}
	}
}
