// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

func TestAnchors(t *testing.T) {
	t.Parallel()

	// Grid order, with each anchor's expected horizontal and vertical placement stated independently.
	rows := []struct {
		name string
		h    assembly.HorizontalPlacement
		v    assembly.VerticalPlacement
	}{
		{"top-left", assembly.PlaceLeft, assembly.PlaceTop},
		{"top", assembly.PlaceCenter, assembly.PlaceTop},
		{"top-right", assembly.PlaceRight, assembly.PlaceTop},
		{"left", assembly.PlaceLeft, assembly.PlaceMiddle},
		{"center", assembly.PlaceCenter, assembly.PlaceMiddle},
		{"right", assembly.PlaceRight, assembly.PlaceMiddle},
		{"bottom-left", assembly.PlaceLeft, assembly.PlaceBottom},
		{"bottom", assembly.PlaceCenter, assembly.PlaceBottom},
		{"bottom-right", assembly.PlaceRight, assembly.PlaceBottom},
	}

	names := assembly.AnchorNames()
	if len(names) != len(rows) {
		t.Fatalf("AnchorNames() = %v", names)
	}

	for index, row := range rows {
		anchor, err := assembly.ParseAnchor(row.name)
		placed := anchor.Horizontal() == row.h && anchor.Vertical() == row.v

		if err != nil || anchor.String() != row.name || !placed || names[index] != row.name {
			t.Errorf("%s = %v, %v, %v, %v", row.name, anchor, anchor.Horizontal(), anchor.Vertical(), err)
		}
	}
}

func TestAnchorRejectsOtherSpellings(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "Top", "TOP", "middle", "top left", "top-left "} {
		got, err := assembly.ParseAnchor(text)
		if !errors.Is(err, assembly.ErrUnknownName) {
			t.Errorf("ParseAnchor(%q) = %v, %v", text, got, err)
		}
	}

	if got := assembly.Anchor(0).String(); got != "anchor(0)" {
		t.Errorf(stringValueFormat, got)
	}
}

func TestAlignAndOverflowNames(t *testing.T) {
	t.Parallel()

	for _, name := range assembly.AlignNames() {
		align, err := assembly.ParseTextAlign(name)
		if err != nil || align.String() != name {
			t.Errorf("%s = %v, %v", name, align, err)
		}
	}

	for _, name := range assembly.OverflowNames() {
		policy, err := assembly.ParseOverflow(name)
		if err != nil || policy.String() != name {
			t.Errorf("%s = %v, %v", name, policy, err)
		}
	}

	got := strings.Join(assembly.AlignNames(), ",") + ";" + strings.Join(assembly.OverflowNames(), ",")
	if got != "left,center,right,justify;error,allow" {
		t.Errorf("names = %s", got)
	}
}

func TestAlignAndOverflowRejectOtherSpellings(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", "Left", "start", "left "} {
		_, err := assembly.ParseTextAlign(text)
		if !errors.Is(err, assembly.ErrUnknownName) {
			t.Errorf("ParseTextAlign(%q) = %v", text, err)
		}
	}

	for _, text := range []string{"", "Error", "clip", "allow "} {
		_, err := assembly.ParseOverflow(text)
		if !errors.Is(err, assembly.ErrUnknownName) {
			t.Errorf("ParseOverflow(%q) = %v", text, err)
		}
	}
}

func TestUnknownEnumerationValuesFormatAsTheirNumber(t *testing.T) {
	t.Parallel()

	got := []string{assembly.TextAlign(0).String(), assembly.Overflow(0).String(), assembly.ItemKind(0).String()}

	if !slices.Equal(got, []string{"align(0)", "overflow(0)", "kind(0)"}) {
		t.Errorf("unknown values must format as their number, got %v", got)
	}
}

func TestFont(t *testing.T) {
	t.Parallel()

	var builtIn assembly.Font
	if !builtIn.IsDefault() || builtIn.String() != "default" {
		t.Errorf("zero font = %+v", builtIn)
	}

	font, err := assembly.FontFile("fonts/A.ttf", baseDirectory)
	if err != nil || font.IsDefault() || font.String() != "fonts/A.ttf" || font.Base != baseDirectory {
		t.Errorf("FontFile = %+v, %v", font, err)
	}

	for _, file := range []string{"", "a\x00b"} {
		_, fileErr := assembly.FontFile(file, baseDirectory)
		if !errors.Is(fileErr, assembly.ErrInvalidPath) {
			t.Errorf("FontFile(%q) = %v", file, fileErr)
		}
	}
}

func TestJoinDir(t *testing.T) {
	t.Parallel()

	absolute := t.TempDir() // an absolute path on every platform

	for _, row := range []struct{ dir, name, want string }{
		{"", aPath, aPath},
		{"d", "", "d"},
		{"d", aPath, "d/a.pdf"},
		{"/r", absolute, absolute},
		{"/r", "x/y", "/r/x/y"},
		{"", "", ""},
		{"a/b", "../c", "a/b/../c"},
	} {
		if got := assembly.JoinDir(row.dir, row.name); got != row.want {
			t.Errorf("JoinDir(%q, %q) = %q; want %q", row.dir, row.name, got, row.want)
		}
	}
}

func TestCheckPathText(t *testing.T) {
	t.Parallel()

	if assembly.CheckPathText("a b/ü.pdf") != nil || !errors.Is(assembly.CheckPathText("a\x00"), assembly.ErrInvalidPath) {
		t.Error("only a NUL character is rejected")
	}
}
