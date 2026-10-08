// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goitalic"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestSuppliedItalicBearingAndEmptyInk(t *testing.T) {
	t.Parallel()

	font, err := typeset.LoadFont(goitalic.TTF)
	if err != nil {
		t.Fatal(err)
	}

	var shaper typeset.Shaper

	p := typeset.Params{Text: "f", Size: 100, PageWidth: 400, PageHeight: 400, Anchor: typeset.AnchorTopRight, OffsetY: -20, LineSpacing: 1}
	placed, err := shaper.Place(font, p)

	var overflow *typeset.OverflowError
	if !errors.As(err, &overflow) || placed == nil || placed.InkBounds == nil || placed.InkBounds.X+placed.InkBounds.Width <= 400 {
		t.Fatalf("italic right overhang escaped: %+v %v", placed, err)
	}

	checkWhitespaceBlockOverflow(t, font)
}

func checkWhitespaceBlockOverflow(t *testing.T, font *typeset.Font) {
	t.Helper()

	var shaper typeset.Shaper

	params := typeset.Params{Size: 100, PageWidth: 1, PageHeight: 1, OffsetX: 100, OffsetY: 100, LineSpacing: 1}
	for _, text := range []string{"", " ", "\n", " \n "} {
		params.Text = text
		params.PageWidth = 1
		params.PageHeight = 1
		params.OffsetX = 100
		params.OffsetY = 100

		empty, placementErr := shaper.Place(font, params)
		if empty == nil || empty.InkBounds != nil {
			t.Fatalf("empty visible ink %q: %+v %v", text, empty, placementErr)
		}

		if text == "" && placementErr != nil {
			t.Fatal(placementErr)
		}

		if text != "" && placementErr == nil {
			t.Fatal("nonempty whitespace logical block escaped overflow")
		}
	}
}

func TestStackedCombiningInkRejectsTopAndBottomClipping(t *testing.T) {
	t.Parallel()
	font := defaultFont(t)

	for _, row := range []struct {
		text   string
		anchor typeset.Anchor
	}{
		{text: "x" + strings.Repeat("\u0301", 8), anchor: typeset.AnchorTopLeft},
		{text: "x" + strings.Repeat("\u0323", 8), anchor: typeset.AnchorBottomLeft},
	} {
		var shaper typeset.Shaper

		params := typeset.Params{
			Text:        row.text,
			Size:        100,
			PageWidth:   400,
			PageHeight:  400,
			Anchor:      row.anchor,
			OffsetX:     50,
			LineSpacing: 1,
		}
		placed, err := shaper.Place(font, params)

		var overflow *typeset.OverflowError
		if !errors.As(err, &overflow) || placed == nil || placed.InkBounds == nil {
			t.Fatalf("stacked combining ink clipping escaped: %+v %v", placed, err)
		}

		params.Overflow = typeset.OverflowAllow

		allowed, err := shaper.Place(font, params)
		if err != nil {
			t.Fatal(err)
		}

		if *placed.InkBounds != *allowed.InkBounds {
			t.Fatal("combining overflow policy changed glyph ink")
		}
	}
}
