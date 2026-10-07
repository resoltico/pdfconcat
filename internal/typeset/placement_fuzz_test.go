// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset_test

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

// fuzzSlack absorbs rounding in the geometric comparisons of checkPlaced.
const fuzzSlack = 1e-6

// FuzzPlace checks invariants of text layout for arbitrary text and numbers: no panic, determinism,
// bounds that contain every line, wrapping within the wrap width unless a word is reported too wide,
// and bounds inside the page unless overflow is allowed or reported.
func FuzzPlace(f *testing.F) {
	seeds := []string{"", "a", "Rīgas ļoti", "a\nb\n", "q̄ x́", "aaa bbb ccc ddd", "Привет мир Ελληνικά", "a\tb", "\xff", "مرحبا"}
	for _, seed := range seeds {
		f.Add(seed, 12.0, 200.0, 200.0, 0.0, 0.0, 60.0, 1.0, 0, 0, false)
	}

	f.Add("aaa bbb ccc ddd eee", 10.0, 100.0, 100.0, -5.0, 3.0, 40.0, 1.5, 4, 3, true)

	font, err := typeset.LoadDefaultFont()
	if err != nil {
		f.Fatal(err)
	}

	var shaper typeset.Shaper

	f.Fuzz(func(t *testing.T, text string, size, pageW, pageH, offX, offY, wrap, spacing float64, anchor, align int, allow bool) {
		params := typeset.Params{
			Text: text, Size: size, PageWidth: pageW, PageHeight: pageH, Anchor: typeset.Anchor(anchor),
			OffsetX: offX, OffsetY: offY, WrapWidth: wrap, Align: typeset.Align(align), LineSpacing: spacing,
		}
		if allow {
			params.Overflow = typeset.OverflowAllow
		}

		placed, placeErr := shaper.Place(font, params)
		if placeErr != nil {
			checkRejection(t, placeErr, placed, params.Overflow)

			return
		}

		again, againErr := shaper.Place(font, params)
		if againErr != nil || !reflect.DeepEqual(placed, again) {
			t.Fatalf("placement is not deterministic: %v", againErr)
		}

		checkPlaced(t, params, placed)
	})
}

func checkRejection(t *testing.T, err error, placed *typeset.Placed, policy typeset.OverflowPolicy) {
	t.Helper()

	var (
		overflow *typeset.OverflowError
		text     *typeset.TextError
		invalid  *typeset.InvalidParamError
	)

	switch {
	case errors.As(err, &overflow):
		if policy == typeset.OverflowAllow || placed == nil || len(overflow.Findings) == 0 {
			t.Fatalf("overflow error without findings or under allow: %v", err)
		}
	case errors.As(err, &text), errors.As(err, &invalid):
		if placed != nil {
			t.Fatalf("result returned with %v", err)
		}
	default:
		t.Fatalf("unclassified error: %v", err)
	}
}

func checkPlaced(t *testing.T, params typeset.Params, placed *typeset.Placed) {
	t.Helper()

	bounds := placed.Bounds
	for _, value := range []float64{bounds.X, bounds.Y, bounds.Width, bounds.Height} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("non-finite bounds %+v", bounds)
		}
	}

	checkLines(t, params, placed)

	if len(placed.Findings) == 0 && len(placed.Lines) > 0 {
		inside := bounds.X >= -fuzzSlack && bounds.Y >= -fuzzSlack &&
			bounds.X+bounds.Width <= params.PageWidth+fuzzSlack && bounds.Y+bounds.Height <= params.PageHeight+fuzzSlack
		if !inside {
			t.Fatalf("bounds %+v outside the %vx%v page without a finding", bounds, params.PageWidth, params.PageHeight)
		}
	}
}

// checkLines verifies every line against the bounds and the wrap width, and that the shown text keeps every
// source character.
func checkLines(t *testing.T, params typeset.Params, placed *typeset.Placed) {
	t.Helper()

	bounds := placed.Bounds
	wide := slices.ContainsFunc(placed.Findings, func(finding typeset.Finding) bool {
		return finding.Kind == typeset.FindingWordTooWide
	})

	var shown strings.Builder

	for _, line := range placed.Lines {
		shown.WriteString(line.Text())

		if line.X < bounds.X-fuzzSlack || line.X+line.Width > bounds.X+bounds.Width+fuzzSlack {
			t.Fatalf("line outside bounds: %+v in %+v", line, bounds)
		}

		if params.WrapWidth > 0 && !wide && line.Width > params.WrapWidth*(1+fuzzSlack) {
			t.Fatalf("line %v wider than wrap width %v without a finding", line.Width, params.WrapWidth)
		}
	}

	// Source characters are never lost: the shown text is the input minus line breaks and the spaces
	// consumed at wrap points.
	got := strings.ReplaceAll(strings.ReplaceAll(shown.String(), " ", ""), "\u00a0", "")
	want := strings.NewReplacer(" ", "", "\u00a0", "", "\r", "", "\n", "").Replace(params.Text)

	if got != want {
		t.Fatalf("text changed: %q -> %q", params.Text, shown.String())
	}
}
