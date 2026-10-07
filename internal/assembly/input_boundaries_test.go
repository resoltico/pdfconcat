// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly_test

import (
	"errors"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const (
	unitHint     = "optional unit pt, mm, cm, or in"
	pageSizeHint = "WIDTHxHEIGHT[unit]"
)

func TestParseLengthDistinguishesAMalformedNumberFromAnUnknownUnit(t *testing.T) {
	t.Parallel()

	// Each input is malformed by syntax, so the message must be the syntax message rather than a range message
	// that would come from converting a half-parsed value.
	for _, text := range []string{unsupportedUnitText, "mm", "-mm", ".", "5.mm", "5e", "5 mm"} {
		_, err := assembly.ParseLength(text)
		if !errors.Is(err, assembly.ErrInvalidLength) || !contains(err.Error(), unitHint) {
			t.Errorf("ParseLength(%q) = %v", text, err)
		}
	}
}

func TestParsePageSizeDistinguishesSyntaxFromRange(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"10x20km", "10x20MM", "10xmm", "10x", "x20", "10x20 mm", "10y20", "10x.mm"} {
		_, err := assembly.ParsePageSize(text)
		if !errors.Is(err, assembly.ErrInvalidPageSize) || !contains(err.Error(), pageSizeHint) {
			t.Errorf("ParsePageSize(%q) = %v", text, err)
		}
	}
}

func TestResolveAcceptsTheFirstAndLastChoices(t *testing.T) {
	t.Parallel()

	anchors := []assembly.Anchor{assembly.AnchorTopLeft, assembly.AnchorBottomRight}
	aligns := []assembly.TextAlign{assembly.AlignLeft, assembly.AlignJustify}

	for _, anchor := range anchors {
		style := assembly.BlankStyle{Text: assembly.TextStyle{Anchor: assembly.Set(anchor, itemOrigin())}}

		spec, err := style.Resolve(a4Dim())
		if err != nil || spec.Text.Anchor != anchor {
			t.Errorf("anchor %v: %+v, %v", anchor, spec.Text, err)
		}
	}

	for _, align := range aligns {
		style := assembly.BlankStyle{Text: assembly.TextStyle{Align: assembly.Set(align, itemOrigin())}}

		spec, err := style.Resolve(a4Dim())
		if err != nil || spec.Text.Align != align {
			t.Errorf("alignment %v: %+v, %v", align, spec.Text, err)
		}
	}
}

func TestClassifyPathRecognizesDriveLettersAtTheEdgesOfTheAlphabet(t *testing.T) {
	t.Parallel()

	rows := []struct {
		path string
		want assembly.PathClass
	}{
		{"a:x", assembly.PathDriveRelative},
		{"z:x", assembly.PathDriveRelative},
		{"A:x", assembly.PathDriveRelative},
		{"Z:x", assembly.PathDriveRelative},
		{"`:x", assembly.PathRelative},
		{"{:x", assembly.PathRelative},
		{"@:x", assembly.PathRelative},
		{"[:x", assembly.PathRelative},
	}

	for _, row := range rows {
		if got := assembly.ClassifyPath(assembly.PathStyleWindows, row.path); got != row.want {
			t.Errorf("ClassifyPath(%q) = %d, want %d", row.path, got, row.want)
		}
	}
}
