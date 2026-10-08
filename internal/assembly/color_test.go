// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"errors"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestParseColor(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]assembly.Color{
		"#000": {}, "#fff": {R: 255, G: 255, B: 255}, "#F4EFE6": {R: 0xF4, G: 0xEF, B: 0xE6}, "#abc": {R: 0xAA, G: 0xBB, B: 0xCC},
		"#aBcDeF": {R: 0xAB, G: 0xCD, B: 0xEF},
	} {
		got, err := assembly.ParseColor(text)
		if err != nil || got != want {
			t.Errorf("ParseColor(%q) = %v, %v; want %v", text, got, err, want)
		}
	}

	if got := (assembly.Color{R: 0xAB, G: 0x0C, B: 0xEF}).String(); got != "#AB0CEF" {
		t.Errorf(stringValueFormat, got)
	}
}

func TestParseColorRejects(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"", "#", "000", shortColor, "#1234", "#12345",
		"#1234567", "#ggg", "#12345g", "# 123", "#+12", "#ééé", "red", "#0x1",
	} {
		got, err := assembly.ParseColor(text)
		if !errors.Is(err, assembly.ErrInvalidColor) {
			t.Errorf("ParseColor(%q) = %v, %v; want ErrInvalidColor", text, got, err)
		}
	}
}

func TestParseFill(t *testing.T) {
	t.Parallel()

	none, err := assembly.ParseFill(noFillName)
	if err != nil || none.Painted || none != (assembly.Fill{}) || none.String() != noFillName {
		t.Errorf("none = %+v, %v", none, err)
	}

	painted, err := assembly.ParseFill("#fa0")
	if err != nil || !painted.Painted || painted.Color != (assembly.Color{R: 0xFF, G: 0xAA}) || painted.String() != "#FFAA00" {
		t.Errorf("#fa0 = %+v, %v", painted, err)
	}

	for _, text := range []string{"", "None", "NONE", " none", "transparent", shortColor} {
		got, fillErr := assembly.ParseFill(text)
		if !errors.Is(fillErr, assembly.ErrInvalidColor) {
			t.Errorf("ParseFill(%q) = %v, %v; want ErrInvalidColor", text, got, fillErr)
		}
	}
}
