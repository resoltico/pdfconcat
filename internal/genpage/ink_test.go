// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package genpage_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goitalic"

	"github.com/resoltico/pdfconcat/internal/genpage"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func TestInkBearingRejectsClippingAndReportsSameRejectedGeometry(t *testing.T) {
	t.Parallel()
	font := defaultFont(t)
	spec := params("j")
	spec.Size = 100
	spec.PageWidth = 400
	spec.PageHeight = 400
	spec.OffsetX = 0
	spec.OffsetY = -20

	var shaper typeset.Shaper

	rejected, err := shaper.Place(font, spec)

	var overflow *typeset.OverflowError
	if !errors.As(err, &overflow) || rejected == nil || rejected.InkBounds == nil || rejected.InkBounds.X >= 0 {
		t.Fatalf("negative glyph bearing escaped rejection: %+v %v", rejected, err)
	}

	spec.Overflow = typeset.OverflowAllow

	allowed, err := shaper.Place(font, spec)
	if err != nil {
		t.Fatal(err)
	}

	if rejected.Bounds != allowed.Bounds || *rejected.InkBounds != *allowed.InkBounds {
		t.Fatal("overflow policy changed computed geometry")
	}

	clipped, _ := writePDF(t, []genpage.Page{{Width: 400, Height: 400, Text: allowed}})
	spec.OffsetX = 20
	spec.Overflow = typeset.OverflowReject
	inside := placed(t, font, spec)
	whole, _ := writePDF(t, []genpage.Page{{Width: 400, Height: 400, Text: inside}})
	a, b := renderGray(t, clipped), renderGray(t, whole)

	if darkPixels(b)-darkPixels(a) < 400 {
		t.Fatalf("independent clipping control failed: %d vs %d ink pixels", darkPixels(a), darkPixels(b))
	}

	ink, found := b.ink(128)
	if !found {
		t.Fatal("no rendered ink")
	}

	bounds := inside.InkBounds
	if math.Abs(float64(ink.left)/4-bounds.X) > 1 || math.Abs(float64(ink.right+1)/4-(bounds.X+bounds.Width)) > 1 {
		t.Fatalf("reported positioned ink does not match independent raster: %+v %+v", bounds, ink)
	}
}

func TestIndependentRasterDetectsTopBottomAndItalicRightClipping(t *testing.T) {
	t.Parallel()
	font := defaultFont(t)

	italic, err := typeset.LoadFont(goitalic.TTF)
	if err != nil {
		t.Fatal(err)
	}

	for _, row := range []struct {
		font         *typeset.Font
		text         string
		anchor       typeset.Anchor
		moveX, moveY float64
	}{
		{font: font, text: "x" + strings.Repeat("\u0301", 8), anchor: typeset.AnchorTopLeft, moveY: -150},
		{font: font, text: "x" + strings.Repeat("\u0323", 8), anchor: typeset.AnchorBottomLeft, moveY: 150},
		{font: italic, text: "f", anchor: typeset.AnchorTopRight, moveX: -30},
	} {
		assertInkClippingDetected(t, row.font, row.text, row.anchor, row.moveX, row.moveY)
	}
}

func assertInkClippingDetected(t *testing.T, font *typeset.Font, text string, anchor typeset.Anchor, moveX, moveY float64) {
	t.Helper()

	params := typeset.Params{
		Text:        text,
		Size:        100,
		PageWidth:   400,
		PageHeight:  400,
		Anchor:      anchor,
		LineSpacing: 1,
		Overflow:    typeset.OverflowAllow,
	}

	var shaper typeset.Shaper

	clipped, err := shaper.Place(font, params)
	if err != nil {
		t.Fatal(err)
	}

	if len(clipped.Findings) == 0 {
		t.Fatal("ink clipping not reported")
	}

	cut, _ := writePDF(t, []genpage.Page{{Width: 400, Height: 400, Text: clipped}})
	params.OffsetX, params.OffsetY = moveX, moveY
	params.Overflow = typeset.OverflowReject
	inside := placed(t, font, params)

	whole, _ := writePDF(t, []genpage.Page{{Width: 400, Height: 400, Text: inside}})
	if darkPixels(renderGray(t, whole)) <= darkPixels(renderGray(t, cut)) {
		t.Fatalf("independent raster did not detect clipping of %q", text)
	}
}

func darkPixels(image bitmap) int {
	count := 0

	for _, pixel := range image.pix {
		if pixel < 128 {
			count++
		}
	}

	return count
}
