// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"math"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func TestDigitCellsCoverInkAndHaveContextIndependentWidth(t *testing.T) {
	font := defaultFont(t)
	digits, err := typeset.NewDigits(font, 12)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"0", "1", "9", "10", "111111", "9876543210", "1000000000000000000"} {
		placed, err := digits.Place(text, 1e7, 42)
		if err != nil {
			t.Fatal(err)
		}
		if placed.Bounds.Width != float64(len(text))*digits.CellWidth || placed.Bounds.X+placed.Bounds.Width != 1e7 {
			t.Fatalf("numeric cell width: %+v", placed.Bounds)
		}
		if placed.InkBounds == nil {
			t.Fatal("digits have no ink")
		}
		ink := placed.InkBounds
		if ink.X < placed.Bounds.X || ink.X+ink.Width > 1e7+1e-8 || ink.Y < 42+digits.Bottom || ink.Y+ink.Height > 42+digits.Top {
			t.Fatalf("digit envelope failed: %q %+v %+v", text, placed.Bounds, ink)
		}
		if placed.Lines[0].Text() != text || len(placed.Lines[0].Clusters) != 1 {
			t.Fatalf("lost contiguous numeric text: %+v", placed.Lines)
		}
	}
	for _, text := range []string{"", "1a", "١", "-1"} {
		if _, err = digits.Place(text, 20, 20); err == nil {
			t.Fatalf("invalid numeric text accepted: %q", text)
		}
	}
	if _, err = digits.Place("1", math.Inf(1), 0); err == nil {
		t.Fatal("nonfinite coordinate accepted")
	}
}
