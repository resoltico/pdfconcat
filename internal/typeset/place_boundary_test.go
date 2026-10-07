// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package typeset_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

type floatLimit struct {
	set      func(*typeset.Params, float64)
	field    string
	accepted []float64
	rejected []float64
}

const (
	// edgeTolerance is the rounding slack of the page-edge checks, in points.
	edgeTolerance = 1e-6
	// fitSlack is the relative slack of the word-too-wide check.
	fitSlack = 1e-9

	languageTagLimit = 35

	fieldPageWidth   = "page width"
	fieldOffsetX     = "offset x"
	fieldWrapWidth   = "wrap width"
	fieldLineSpacing = "line spacing"
	fieldAnchor      = "anchor"
	fieldLanguage    = "language"
	fieldText        = "text"
)

// rejectedField returns the field named by the InvalidParamError of params, or "" when Place accepts the
// parameters.
func rejectedField(tb testing.TB, params typeset.Params) string {
	tb.Helper()

	var shaper typeset.Shaper

	_, err := shaper.Place(defaultFont(tb), params)

	invalid, ok := errors.AsType[*typeset.InvalidParamError](err)
	if ok {
		return invalid.Field
	}

	if err != nil {
		overflowErr, overflow := errors.AsType[*typeset.OverflowError](err)
		if !overflow || overflowErr == nil {
			tb.Fatalf("unexpected error: %v", err)
		}
	}

	return ""
}

func hasFinding(placed *typeset.Placed, kind typeset.FindingKind) bool {
	for _, finding := range placed.Findings {
		if finding.Kind == kind {
			return true
		}
	}

	return false
}

func floatLimits() []floatLimit {
	above := func(limit float64) float64 { return math.Nextafter(limit, math.Inf(1)) }
	below := func(limit float64) float64 { return math.Nextafter(limit, math.Inf(-1)) }

	return []floatLimit{
		{
			func(p *typeset.Params, v float64) { p.Size = v }, fieldSize,
			[]float64{typeset.MinFontSize, typeset.MaxFontSize},
			[]float64{below(typeset.MinFontSize), above(typeset.MaxFontSize), 0, -1},
		},
		{
			func(p *typeset.Params, v float64) { p.PageWidth = v }, fieldPageWidth,
			[]float64{typeset.MinPageSide, typeset.MaxPageSide},
			[]float64{below(typeset.MinPageSide), above(typeset.MaxPageSide)},
		},
		{
			func(p *typeset.Params, v float64) { p.PageHeight = v }, "page height",
			[]float64{typeset.MinPageSide, typeset.MaxPageSide},
			[]float64{below(typeset.MinPageSide), above(typeset.MaxPageSide)},
		},
		{
			func(p *typeset.Params, v float64) { p.OffsetX = v }, fieldOffsetX,
			[]float64{typeset.MaxOffset, -typeset.MaxOffset, 0},
			[]float64{above(typeset.MaxOffset), -above(typeset.MaxOffset)},
		},
		{
			func(p *typeset.Params, v float64) { p.OffsetY = v }, "offset y",
			[]float64{typeset.MaxOffset, -typeset.MaxOffset, 0},
			[]float64{above(typeset.MaxOffset), -above(typeset.MaxOffset)},
		},
		{
			func(p *typeset.Params, v float64) { p.WrapWidth = v }, fieldWrapWidth,
			[]float64{0, math.SmallestNonzeroFloat64, typeset.MaxOffset},
			[]float64{-math.SmallestNonzeroFloat64, above(typeset.MaxOffset)},
		},
		{
			func(p *typeset.Params, v float64) { p.LineSpacing = v }, fieldLineSpacing,
			[]float64{math.SmallestNonzeroFloat64, typeset.MaxLineSpacing},
			[]float64{0, -math.SmallestNonzeroFloat64, above(typeset.MaxLineSpacing)},
		},
	}
}

func TestNumericParameterLimitsAreExact(t *testing.T) {
	t.Parallel()

	for _, tc := range floatLimits() {
		for _, value := range tc.accepted {
			params := baseParams("x")
			tc.set(&params, value)

			if got := rejectedField(t, params); got != "" {
				t.Errorf("%s %v rejected as %q", tc.field, value, got)
			}
		}

		for _, value := range tc.rejected {
			params := baseParams("x")
			tc.set(&params, value)

			if got := rejectedField(t, params); got != tc.field {
				t.Errorf("%s %v: rejected as %q", tc.field, value, got)
			}
		}
	}
}

func TestEnumeratedParameterLimitsAreExact(t *testing.T) {
	t.Parallel()

	cases := []struct {
		set      func(*typeset.Params, int)
		field    string
		accepted []int
		rejected []int
	}{
		{
			func(p *typeset.Params, v int) { p.Anchor = typeset.Anchor(v) }, fieldAnchor,
			[]int{int(typeset.AnchorTopLeft), int(typeset.AnchorBottomRight)},
			[]int{-1, int(typeset.AnchorBottomRight) + 1},
		},
		{
			func(p *typeset.Params, v int) { p.Align = typeset.Align(v) }, "align",
			[]int{int(typeset.AlignLeft), int(typeset.AlignJustify)},
			[]int{-1, int(typeset.AlignJustify) + 1},
		},
		{
			func(p *typeset.Params, v int) { p.Overflow = typeset.OverflowPolicy(v) }, "overflow",
			[]int{int(typeset.OverflowReject), int(typeset.OverflowAllow)},
			[]int{-1, int(typeset.OverflowAllow) + 1},
		},
	}

	for _, tc := range cases {
		for _, value := range tc.accepted {
			params := baseParams("x")
			tc.set(&params, value)

			if got := rejectedField(t, params); got != "" {
				t.Errorf("%s %d rejected as %q", tc.field, value, got)
			}
		}

		for _, value := range tc.rejected {
			params := baseParams("x")
			tc.set(&params, value)

			if got := rejectedField(t, params); got != tc.field {
				t.Errorf("%s %d: rejected as %q", tc.field, value, got)
			}
		}
	}
}

func TestLanguageAndTextLengthLimitsAreExact(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		want     string
		language string
		text     string
	}{
		"longest tag":          {"", strings.Repeat("a", languageTagLimit), "x"},
		"tag one too long":     {fieldLanguage, strings.Repeat("a", languageTagLimit+1), "x"},
		"all legal characters": {"", "azAZ09-", "x"},
		"before a":             {fieldLanguage, "`", "x"},
		"after z":              {fieldLanguage, "{", "x"},
		"before A":             {fieldLanguage, "@", "x"},
		"after Z":              {fieldLanguage, "[", "x"},
		"before 0":             {fieldLanguage, "/", "x"},
		"after 9":              {fieldLanguage, ":", "x"},
		"maximum text":         {"", "", strings.Repeat("a ", typeset.MaxTextRunes/2)},
		"text one too long":    {fieldText, "", strings.Repeat("a ", typeset.MaxTextRunes/2) + "a"},
		"multibyte text":       {"", "", strings.Repeat("ā", typeset.MaxTextRunes)},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			params := baseParams(tc.text)
			params.Language = tc.language

			if got := rejectedField(t, params); got != tc.want {
				t.Errorf("rejected as %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLanguageSelectsRomanianCommaBelow(t *testing.T) {
	t.Parallel()

	glyphOfText := func(text, language string) uint16 {
		params := baseParams(text)
		params.Language = language

		return place(t, params).Lines[0].Clusters[0].Glyphs[0].ID
	}

	// With Romanian as the language, the cedilla letter shapes as the comma-below letter.
	if got, want := glyphOfText("ş", "ro"), glyphOfText("ș", ""); got != want {
		t.Errorf("Romanian ş shaped to glyph %d, want the comma-below glyph %d", got, want)
	}

	if glyphOfText("ş", "") == glyphOfText("ş", "ro") {
		t.Error("the language tag changed nothing")
	}
}

func TestMissingGlyphOffsetCountsEveryEarlierParagraph(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	_, err := shaper.Place(defaultFont(t), baseParams("ab\ncd\nef ☃"))

	textErr, ok := errors.AsType[*typeset.TextError](err)
	if !ok || textErr.Problems[0].Offset != 9 {
		t.Errorf("offset of the missing glyph: %v", err)
	}
}

func TestSpaceRunCountsFullyWhenFittingAWord(t *testing.T) {
	t.Parallel()

	two := place(t, baseParams("111 222")).Lines[0].Width

	p := baseParams("111    222")
	p.WrapWidth = two + 0.01

	if got := lineTexts(place(t, p)); got != "111|222" {
		t.Errorf("got %q: four spaces are wider than the one that fits", got)
	}
}

func TestWordTooWideToleranceIsRelativeAndExact(t *testing.T) {
	t.Parallel()

	params := baseParams("x")
	params.Size = 10
	natural := place(t, params).Lines[0].Width

	// The widest box for which the word still counts as too wide is where wrap*(1+slack) falls just below
	// the word; find the box where it equals the word exactly.
	wrap := natural / (1 + fitSlack)
	for range 16 {
		if wrap*(1+fitSlack) >= natural {
			break
		}

		wrap = math.Nextafter(wrap, math.Inf(1))
	}

	if wrap*(1+fitSlack) != natural {
		t.Fatalf("no box width puts the word exactly on the tolerance edge (%v)", natural)
	}

	params.WrapWidth = wrap
	if placed := place(t, params); len(placed.Findings) != 0 {
		t.Errorf("a word within the tolerance is flagged: %v", placed.Findings)
	}

	params.WrapWidth = wrap * (1 - 1e-6)
	if placed := place(t, params); len(placed.Findings) != 1 || placed.Findings[0].Kind != typeset.FindingWordTooWide {
		t.Errorf("a word beyond the tolerance is not flagged: %v", placed.Findings)
	}
}

func TestWordTooWideFindingNamesItsLine(t *testing.T) {
	t.Parallel()

	params := baseParams("aa\nextraordinarily")
	params.WrapWidth = 30
	placed := place(t, params)

	if len(placed.Findings) != 1 {
		t.Fatalf("findings %v", placed.Findings)
	}

	finding := placed.Findings[0]
	want := fmt.Sprintf("line 2 is %.2f pt wide, the box is 30.00 pt", placed.Lines[1].Width)

	if finding.Kind != typeset.FindingWordTooWide || finding.Line != 1 || finding.Detail != want {
		t.Errorf("finding %+v, want line 1 and detail %q", finding, want)
	}
}

func TestJustificationAddsExtraSpaceToEveryInteriorSpace(t *testing.T) {
	t.Parallel()

	const wrap = 100.0

	text := "aaa bbb ccc ddd eee fff ggg hhh iii jjj kkk"

	params := baseParams(text)
	params.Size, params.WrapWidth = 10, wrap
	natural := place(t, params)

	params.Align = typeset.AlignJustify
	justified := place(t, params)

	if len(justified.Lines) < 2 {
		t.Fatalf("lines %q", lineTexts(justified))
	}

	for index, line := range justified.Lines[:len(justified.Lines)-1] {
		spaces := strings.Count(strings.Trim(line.Text(), " "), " ")
		gap := wrap - natural.Lines[index].Width

		if spaces == 0 || !near(line.ExtraSpace*float64(spaces), gap) || line.ExtraSpace <= 0 {
			t.Errorf("line %d %q: %d interior spaces, extra %v, gap %v", index, line.Text(), spaces, line.ExtraSpace, gap)
		}
	}

	if last := justified.Lines[len(justified.Lines)-1]; last.ExtraSpace != 0 {
		t.Errorf("last line is justified by %v", last.ExtraSpace)
	}
}

func TestPageEdgeToleranceIsExactOnLeftAndRight(t *testing.T) {
	t.Parallel()

	cases := []struct {
		edit    func(*typeset.Params)
		name    string
		outside bool
	}{
		{func(p *typeset.Params) { p.OffsetX = -edgeTolerance }, "left edge on the tolerance", false},
		{func(p *typeset.Params) { p.OffsetX = -2 * edgeTolerance }, "left edge beyond the tolerance", true},
		{func(p *typeset.Params) { p.OffsetX = edgeTolerance }, "right edge on the tolerance", false},
		{func(p *typeset.Params) { p.OffsetX = 2 * edgeTolerance }, "right edge beyond the tolerance", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := baseParams("x")
			params.PageWidth, params.WrapWidth = 100, 100
			tc.edit(&params)

			if got := hasFinding(place(t, params), typeset.FindingOutsidePageHorizontal); got != tc.outside {
				t.Errorf("outside %v, want %v", got, tc.outside)
			}
		})
	}
}

func TestPageEdgeToleranceIsExactAtTopAndBottom(t *testing.T) {
	t.Parallel()

	// Size 6000 makes the block 8172 points high on a page 14400 high: the subtractions below are exact.
	tall := func(offsetY float64) typeset.Params {
		params := baseParams("x")
		params.Size, params.PageWidth, params.PageHeight, params.OffsetY = 6000, typeset.MaxPageSide, typeset.MaxPageSide, offsetY

		return params
	}

	if hasFinding(place(t, tall(edgeTolerance)), typeset.FindingOutsidePageVertical) {
		t.Error("a top edge on the tolerance is flagged")
	}

	if !hasFinding(place(t, tall(2*edgeTolerance)), typeset.FindingOutsidePageVertical) {
		t.Error("a top edge beyond the tolerance is not flagged")
	}

	// A block resting exactly on the bottom edge is inside, and one point below it is outside.
	bottom := baseParams("x")
	bottom.Anchor = typeset.AnchorBottomLeft

	if got := place(t, bottom); len(got.Findings) != 0 || got.Bounds.Y != 0 {
		t.Errorf("bounds %+v findings %v", got.Bounds, got.Findings)
	}

	bottom.OffsetY = -2 * edgeTolerance
	if !hasFinding(place(t, bottom), typeset.FindingOutsidePageVertical) {
		t.Error("a bottom edge beyond the tolerance is not flagged")
	}
}

func TestPageFindingsDescribeTheSpan(t *testing.T) {
	t.Parallel()

	params := baseParams("x")
	params.WrapWidth, params.OffsetX, params.OffsetY = 100, 150, 10
	placed := place(t, params)

	want := []typeset.Finding{
		{
			Kind: typeset.FindingOutsidePageHorizontal, Line: -1,
			Detail: "text spans x 150.00 to 250.00 pt, the page is 200.00 pt wide",
		},
		{
			Kind: typeset.FindingOutsidePageVertical, Line: -1,
			Detail: "text spans y 193.66 to 210.00 pt, the page is 200.00 pt high",
		},
	}

	if len(placed.Findings) != len(want) {
		t.Fatalf("findings %v", placed.Findings)
	}

	for index := range want {
		if placed.Findings[index] != want[index] {
			t.Errorf("finding %d is %+v, want %+v", index, placed.Findings[index], want[index])
		}
	}
}
