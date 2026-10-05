// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"math"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestParseLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		want    float64
		wantErr bool
	}{
		{text: "12", want: 12},
		{text: "12pt", want: 12},
		{text: " 1in ", want: 72},
		{text: "25.4mm", want: 72},
		{text: "2.54cm", want: 72},
		{text: "-36", want: -36},
		{text: ".5in", want: 36},
		{text: "", wantErr: true},
		{text: "mm", wantErr: true},
		{text: "12px", wantErr: true},
		{text: "NaN", wantErr: true},
		{text: "1e999", wantErr: true},
		{text: "99999", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()

			got, err := assembly.ParseLength(test.text)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseLength(%q) error = %v, wantErr %v", test.text, err, test.wantErr)
			}

			if err == nil && math.Abs(float64(got)-test.want) > 1e-9 {
				t.Fatalf("ParseLength(%q) = %v, want %v", test.text, got, test.want)
			}
		})
	}
}

func TestParseColor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		want    assembly.Color
		wantErr bool
	}{
		{text: "#000000", want: assembly.Color{}},
		{text: "#fff", want: assembly.Color{R: 255, G: 255, B: 255}},
		{text: "#1a2B3c", want: assembly.Color{R: 0x1A, G: 0x2B, B: 0x3C}},
		{text: "red", wantErr: true},
		{text: "#12", wantErr: true},
		{text: "#12345", wantErr: true},
		{text: "#GGGGGG", wantErr: true},
		{text: "#+12345", wantErr: true},
		{text: "", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()

			got, err := assembly.ParseColor(test.text)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseColor(%q) error = %v, wantErr %v", test.text, err, test.wantErr)
			}

			if err == nil && got != test.want {
				t.Fatalf("ParseColor(%q) = %v, want %v", test.text, got, test.want)
			}
		})
	}

	if got := (assembly.Color{R: 0xAB, G: 0xCD, B: 0xEF}).String(); got != "#ABCDEF" {
		t.Fatalf("Color.String() = %q", got)
	}
}

func TestParsePageSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text    string
		wantDim assembly.PageDim
		inherit bool
		wantErr bool
	}{
		{text: "inherit", inherit: true},
		{text: "INHERIT", inherit: true},
		{text: "Letter", wantDim: assembly.PageDim{Width: 612, Height: 792}},
		{text: "a4", wantDim: assembly.PageDim{Width: 595.2755905511812, Height: 841.8897637795276}},
		{text: "8.5x11in", wantDim: assembly.PageDim{Width: 612, Height: 792}},
		{text: "612x792", wantDim: assembly.PageDim{Width: 612, Height: 792}},
		{text: "25.4x50.8mm", wantDim: assembly.PageDim{Width: 72, Height: 144}},
		{text: "A4L", wantErr: true},
		{text: "0x10", wantErr: true},
		{text: "10x", wantErr: true},
		{text: "axb", wantErr: true},
		{text: "210mmx297mm", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			t.Parallel()

			got, err := assembly.ParsePageSize(test.text)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParsePageSize(%q) error = %v, wantErr %v", test.text, err, test.wantErr)
			}

			if err != nil {
				return
			}

			if got.Inherit != test.inherit {
				t.Fatalf("ParsePageSize(%q).Inherit = %v", test.text, got.Inherit)
			}

			if !test.inherit &&
				(math.Abs(float64(got.Dim.Width-test.wantDim.Width)) > 1e-6 || math.Abs(float64(got.Dim.Height-test.wantDim.Height)) > 1e-6) {
				t.Fatalf("ParsePageSize(%q) = %+v, want %+v", test.text, got.Dim, test.wantDim)
			}
		})
	}
}

func TestParseTextValuesAreCaseInsensitiveAndRoundTrip(t *testing.T) {
	t.Parallel()

	font, err := assembly.ParseFont("times-BOLDitalic")
	if err != nil || font != "Times-BoldItalic" {
		t.Fatalf("ParseFont() = %q, %v", font, err)
	}

	if _, err = assembly.ParseFont("Comic Sans"); err == nil {
		t.Fatal("ParseFont() accepted an unknown font")
	}

	for _, name := range []string{"top-left", "top", "top-right", "left", "center", "right", "bottom-left", "bottom", "bottom-right"} {
		anchor, parseErr := assembly.ParseAnchor(name)
		if parseErr != nil || anchor.String() != name {
			t.Fatalf("ParseAnchor(%q) = %v, %v", name, anchor, parseErr)
		}
	}

	if _, err = assembly.ParseAnchor("middle"); err == nil {
		t.Fatal("ParseAnchor() accepted an unknown anchor")
	}

	for _, name := range []string{"left", "center", "right", "justify"} {
		align, parseErr := assembly.ParseTextAlign(name)
		if parseErr != nil || align.String() != name {
			t.Fatalf("ParseTextAlign(%q) = %v, %v", name, align, parseErr)
		}
	}

	if _, err = assembly.ParseTextAlign("full"); err == nil {
		t.Fatal("ParseTextAlign() accepted an unknown alignment")
	}
}

func TestAnchorComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		anchor assembly.Anchor
		h      assembly.HorizontalPlacement
		v      assembly.VerticalPlacement
	}{
		{assembly.AnchorTopLeft, assembly.PlaceLeft, assembly.PlaceTop},
		{assembly.AnchorTop, assembly.PlaceCenter, assembly.PlaceTop},
		{assembly.AnchorTopRight, assembly.PlaceRight, assembly.PlaceTop},
		{assembly.AnchorLeft, assembly.PlaceLeft, assembly.PlaceMiddle},
		{assembly.AnchorCenter, assembly.PlaceCenter, assembly.PlaceMiddle},
		{assembly.AnchorRight, assembly.PlaceRight, assembly.PlaceMiddle},
		{assembly.AnchorBottomLeft, assembly.PlaceLeft, assembly.PlaceBottom},
		{assembly.AnchorBottom, assembly.PlaceCenter, assembly.PlaceBottom},
		{assembly.AnchorBottomRight, assembly.PlaceRight, assembly.PlaceBottom},
	}
	for _, test := range tests {
		if test.anchor.Horizontal() != test.h || test.anchor.Vertical() != test.v {
			t.Errorf("%v = (%v, %v), want (%v, %v)", test.anchor, test.anchor.Horizontal(), test.anchor.Vertical(), test.h, test.v)
		}
	}
}
