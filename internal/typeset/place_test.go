// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

type overflowCase struct {
	edit   func(*typeset.Params)
	name   string
	kinds  []typeset.FindingKind
	inside bool
}

// fieldSize names the font size in InvalidParamError.
const fieldSize = "size"

func place(t *testing.T, p typeset.Params) *typeset.Placed {
	t.Helper()

	var s typeset.Shaper

	placed, err := s.Place(defaultFont(t), p)
	if err != nil {
		t.Fatalf("%q: %v", p.Text, err)
	}

	return placed
}

func lineTexts(p *typeset.Placed) string {
	texts := make([]string, len(p.Lines))
	for i, l := range p.Lines {
		texts[i] = l.Text()
	}

	return strings.Join(texts, "|")
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestLineBreakingRules(t *testing.T) {
	t.Parallel()

	// Digits advance equally, so "111 222" is the width of any two three-digit words on one line.
	two := place(t, baseParams("111 222")).Lines[0].Width

	cases := []struct {
		name string
		text string
		want string
		wrap float64
	}{
		{"fits exactly", "111 222 333 444", "111 222|333 444", two + 0.01},
		{"just too narrow", "111 222 333 444", "111|222|333|444", two - 0.01},
		{"newline", "a\nb", "a|b", 0},
		{"blank line", "a\n\nb", "a||b", 0},
		{"final newline adds nothing", "a\n", "a", 0},
		{"two final newlines", "a\n\n", "a|", 0},
		{"only newline", "\n", "", 0},
		{"empty", "", "", 0},
		{"crlf and cr", "a\r\nb\rc", "a|b|c", 0},
		{"spaces kept without wrapping", "  a  b  ", "  a  b  ", 0},
		{"nbsp never breaks", "111 222\u00a0333 444", "111|222\u00a0333|444", two + 0.01},
		{"leading spaces count", "  111 222", "  111|222", two + 0.01},
		{"space run consumed at break", "111    222", "111|222", two - 0.01},
		{"only spaces", "    ", "    ", 10},
		{"trailing spaces kept on last line", "111 ", "111 ", 100},
		{"paragraphs wrap separately", "111 222 333\n444 555 666", "111 222|333|444 555|666", two + 0.01},
		{"hyphen is not a break", "11-11 22-22", "11-11|22-22", two - 0.01},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := baseParams(tc.text)
			p.WrapWidth = tc.wrap

			if got := lineTexts(place(t, p)); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestAlignmentInsideBlock(t *testing.T) {
	t.Parallel()

	const wrap = 100.0

	text := "aaa bbb ccc ddd eee fff ggg hhh\n\nxx"

	for _, align := range []typeset.Align{typeset.AlignLeft, typeset.AlignCenter, typeset.AlignRight, typeset.AlignJustify} {
		params := baseParams(text)
		params.Size, params.WrapWidth, params.Align = 10, wrap, align
		placed := place(t, params)

		if placed.Bounds.Width != wrap {
			t.Fatalf("block width %v", placed.Bounds.Width)
		}

		for index := range placed.Lines {
			problem := alignmentProblem(align, wrap, placed.Lines, index)
			if problem != "" {
				t.Errorf("%s: line %d: %s", alignName(align), index, problem)
			}
		}
	}
}

func alignName(align typeset.Align) string {
	return [...]string{"left", "center", "right", "justify"}[align]
}

// alignmentProblem describes how the index-th line violates the alignment inside a block of the given
// width, or returns "" when it does not.
func alignmentProblem(align typeset.Align, wrap float64, lines []typeset.Line, index int) string {
	line := lines[index]
	right := line.X + line.Width

	// Last line of each paragraph: the line before a blank one, and the final line.
	paragraphEnd := line.Text() == "" || line.Text() == "xx" || index+1 < len(lines) && lines[index+1].Text() == ""

	switch align {
	case typeset.AlignLeft:
		return mismatch("x", line.X, 0)
	case typeset.AlignCenter:
		return mismatch("x", line.X, (wrap-line.Width)/2)
	case typeset.AlignRight:
		return mismatch(rightEdgeCase, right, wrap)
	case typeset.AlignJustify:
		if paragraphEnd {
			return cmp.Or(mismatch("x", line.X, 0), mismatch("extra space", line.ExtraSpace, 0))
		}

		return mismatch(rightEdgeCase, right, wrap)
	default:
		return "unknown alignment"
	}
}

// mismatch returns "" when got is near want, otherwise a description of the measured value.
func mismatch(what string, got, want float64) string {
	if near(got, want) {
		return ""
	}

	return fmt.Sprintf("%s is %v, want %v", what, got, want)
}

func TestJustifyNeedsInteriorSpaces(t *testing.T) {
	t.Parallel()

	p := baseParams("aaaaaaaa  bbbb")
	p.Size, p.WrapWidth, p.Align = 10, 200, typeset.AlignJustify
	placed := place(t, p)

	if placed.Lines[0].ExtraSpace != 0 {
		t.Error("a paragraph's only line is its last line and is not justified")
	}

	// Leading spaces are not justification points.
	p = baseParams("   aaaaaaaaaaaaaa bbbbbbbbbbbbbbbb ccc")
	p.Size, p.WrapWidth, p.Align = 10, 120, typeset.AlignJustify
	placed = place(t, p)

	if len(placed.Lines) < 2 || placed.Lines[0].ExtraSpace != 0 {
		t.Fatalf("a line with no interior space must not be justified: %q", lineTexts(placed))
	}
}

func TestAnchorsAndOffsets(t *testing.T) {
	t.Parallel()

	// One 100-point block of known height on a 400 x 300 page.
	base := baseParams("x")
	base.Size, base.PageWidth, base.PageHeight, base.WrapWidth = 20, 400, 300, 100

	height := place(t, base).Bounds.Height
	if !near(height, 20*(1069+293)/1000.0) {
		t.Fatalf("height %v", height)
	}

	cases := []struct {
		anchor typeset.Anchor
		x, y   float64 // bottom-left of the bounds
	}{
		{typeset.AnchorTopLeft, 0, 300 - height},
		{typeset.AnchorTopCenter, 150, 300 - height},
		{typeset.AnchorTopRight, 300, 300 - height},
		{typeset.AnchorMiddleLeft, 0, (300 - height) / 2},
		{typeset.AnchorCenter, 150, (300 - height) / 2},
		{typeset.AnchorMiddleRight, 300, (300 - height) / 2},
		{typeset.AnchorBottomLeft, 0, 0},
		{typeset.AnchorBottomCenter, 150, 0},
		{typeset.AnchorBottomRight, 300, 0},
	}

	for _, tc := range cases {
		for _, off := range [][2]float64{{0, 0}, {7.5, 11}, {-3, -4}} {
			p := base
			p.Anchor, p.OffsetX, p.OffsetY = tc.anchor, off[0], off[1]
			placed := place(t, p)
			b := placed.Bounds

			if !near(b.X, tc.x+off[0]) || !near(b.Y, tc.y+off[1]) || !near(b.Width, 100) || !near(b.Height, height) {
				t.Errorf("anchor %d offset %v: bounds %+v", tc.anchor, off, b)
			}

			// The first baseline sits one ascent below the block top.
			if !near(placed.Lines[0].Baseline, b.Y+b.Height-20*1.069) || !near(placed.Lines[0].X, b.X) {
				t.Errorf("anchor %d: line at %v,%v", tc.anchor, placed.Lines[0].X, placed.Lines[0].Baseline)
			}
		}
	}
}

func overflowCases() []overflowCase {
	horizontal := []typeset.FindingKind{typeset.FindingOutsidePageHorizontal}
	vertical := []typeset.FindingKind{typeset.FindingOutsidePageVertical}
	tooWide := []typeset.FindingKind{typeset.FindingWordTooWide}

	return []overflowCase{
		{func(*typeset.Params) {}, "fits", nil, true},
		{func(p *typeset.Params) { p.Text, p.WrapWidth = "short extraordinarily", 30 }, "word too wide", tooWide, false},
		{
			func(p *typeset.Params) { p.Text, p.WrapWidth, p.PageWidth = "extraordinarily", 30, 5000 },
			"word too wide on a wide page", tooWide, false,
		},
		{func(p *typeset.Params) { p.Anchor, p.OffsetX = typeset.AnchorTopRight, 1 }, rightEdgeCase, horizontal, false},
		{func(p *typeset.Params) { p.OffsetX = -1 }, "left edge", horizontal, false},
		{func(p *typeset.Params) { p.OffsetY = 1 }, "top edge", vertical, false},
		{func(p *typeset.Params) { p.Anchor, p.OffsetY = typeset.AnchorBottomLeft, -1 }, "bottom edge", vertical, false},
		{func(p *typeset.Params) { p.Text = strings.Repeat("a\n", 30) }, "too many lines", vertical, false},
		{
			func(p *typeset.Params) { p.OffsetX, p.OffsetY = -50, 50 },
			"both axes",
			[]typeset.FindingKind{typeset.FindingOutsidePageHorizontal, typeset.FindingOutsidePageVertical},
			false,
		},
		{func(p *typeset.Params) { p.PageWidth, p.WrapWidth, p.Text = 100, 100, "x" }, "exactly on the edges", nil, true},
	}
}

func TestOverflowFindings(t *testing.T) {
	t.Parallel()

	for _, tc := range overflowCases() {
		for _, policy := range []typeset.OverflowPolicy{typeset.OverflowAllow, typeset.OverflowReject} {
			t.Run(fmt.Sprintf("%s/policy %d", tc.name, policy), func(t *testing.T) {
				t.Parallel()

				params := baseParams("hello")
				params.Overflow = policy
				tc.edit(&params)

				var shaper typeset.Shaper

				placed, err := shaper.Place(defaultFont(t), params)
				if placed == nil {
					t.Fatalf("no result: %v", err)
				}

				got := make([]typeset.FindingKind, 0, len(placed.Findings))
				for _, finding := range placed.Findings {
					got = append(got, finding.Kind)
				}

				if !slices.Equal(got, tc.kinds) {
					t.Errorf("findings %v, want %v", got, tc.kinds)
				}

				checkOverflowResult(t, tc, policy, err)
			})
		}
	}
}

func checkOverflowResult(t *testing.T, tc overflowCase, policy typeset.OverflowPolicy, err error) {
	t.Helper()

	switch {
	case policy == typeset.OverflowAllow && err != nil:
		t.Errorf("allow policy failed: %v", err)
	case policy == typeset.OverflowReject && tc.inside && err != nil:
		t.Errorf("unexpected %v", err)
	case policy == typeset.OverflowReject && !tc.inside:
		checkOverflowError(t, tc, err)
	default:
	}
}

func checkOverflowError(t *testing.T, tc overflowCase, err error) {
	t.Helper()

	overflow, ok := errors.AsType[*typeset.OverflowError](err)
	if !ok {
		t.Errorf("want OverflowError, got %v", err)

		return
	}

	if len(overflow.Findings) != len(tc.kinds) {
		t.Errorf("error carries %d findings, want %d", len(overflow.Findings), len(tc.kinds))
	}

	if !strings.Contains(err.Error(), string(tc.kinds[0])) || !strings.Contains(err.Error(), `"allow"`) {
		t.Errorf(messageValueFormat, err)
	}
}

func TestEmptyTextNeverOverflows(t *testing.T) {
	t.Parallel()

	p := baseParams("")
	p.OffsetX, p.OffsetY, p.Overflow = -1e6, 1e6, typeset.OverflowReject
	placed := place(t, p)

	if len(placed.Lines) != 0 || placed.Bounds != (typeset.Rect{}) || len(placed.Findings) != 0 {
		t.Errorf("%+v", placed)
	}
}

func TestParamsValidation(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	cases := []struct {
		edit  func(*typeset.Params)
		field string
	}{
		{func(p *typeset.Params) { p.Size = 0 }, fieldSize},
		{func(p *typeset.Params) { p.Size = typeset.MinFontSize / 2 }, fieldSize},
		{func(p *typeset.Params) { p.Size = math.NaN() }, fieldSize},
		{func(p *typeset.Params) { p.Size = math.Inf(1) }, fieldSize},
		{func(p *typeset.Params) { p.Size = typeset.MaxFontSize + 1 }, fieldSize},
		{func(p *typeset.Params) { p.PageWidth = 0.5 }, "page width"},
		{func(p *typeset.Params) { p.PageWidth = math.NaN() }, "page width"},
		{func(p *typeset.Params) { p.PageHeight = typeset.MaxPageSide + 1 }, "page height"},
		{func(p *typeset.Params) { p.Anchor = 9 }, "anchor"},
		{func(p *typeset.Params) { p.Anchor = -1 }, "anchor"},
		{func(p *typeset.Params) { p.OffsetX = typeset.MaxOffset * 2 }, "offset x"},
		{func(p *typeset.Params) { p.OffsetX = math.NaN() }, "offset x"},
		{func(p *typeset.Params) { p.OffsetY = math.Inf(-1) }, "offset y"},
		{func(p *typeset.Params) { p.WrapWidth = -1 }, "wrap width"},
		{func(p *typeset.Params) { p.WrapWidth = typeset.MaxOffset + 1 }, "wrap width"},
		{func(p *typeset.Params) { p.Align = 4 }, "align"},
		{func(p *typeset.Params) { p.LineSpacing = 0 }, "line spacing"},
		{func(p *typeset.Params) { p.LineSpacing = typeset.MaxLineSpacing + 1 }, "line spacing"},
		{func(p *typeset.Params) { p.Overflow = 2 }, "overflow"},
		{func(p *typeset.Params) { p.Language = "lv_LV" }, "language"},
		{func(p *typeset.Params) { p.Language = strings.Repeat("a", 36) }, "language"},
		{func(p *typeset.Params) { p.Text = strings.Repeat("a", typeset.MaxTextRunes+1) }, "text"},
	}

	for _, tc := range cases {
		p := baseParams("x")
		tc.edit(&p)

		_, err := shaper.Place(defaultFont(t), p)

		var invalid *typeset.InvalidParamError
		if !errors.As(err, &invalid) || invalid.Field != tc.field {
			t.Errorf("%s: got %v", tc.field, err)
		}
	}

	_, err := shaper.Place(nil, baseParams("x"))

	var invalid *typeset.InvalidParamError
	if !errors.As(err, &invalid) || invalid.Field != "font" {
		t.Errorf("nil font: %v", err)
	}
}

func TestNumericBoundariesAreAccepted(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*typeset.Params){
		"smallest size":    func(p *typeset.Params) { p.Size = typeset.MinFontSize },
		"largest size":     func(p *typeset.Params) { p.Size = typeset.MaxFontSize },
		"smallest page":    func(p *typeset.Params) { p.PageWidth, p.PageHeight = typeset.MinPageSide, typeset.MinPageSide },
		"largest page":     func(p *typeset.Params) { p.PageWidth, p.PageHeight = typeset.MaxPageSide, typeset.MaxPageSide },
		"largest offsets":  func(p *typeset.Params) { p.OffsetX, p.OffsetY = typeset.MaxOffset, -typeset.MaxOffset },
		"largest wrap":     func(p *typeset.Params) { p.WrapWidth = typeset.MaxOffset },
		"tiny wrap":        func(p *typeset.Params) { p.WrapWidth = 1e-9 },
		"largest spacing":  func(p *typeset.Params) { p.LineSpacing = typeset.MaxLineSpacing },
		"tiny spacing":     func(p *typeset.Params) { p.LineSpacing = 1e-9 },
		"language tag":     func(p *typeset.Params) { p.Language = "lv-LV" },
		"maximum text":     func(p *typeset.Params) { p.Text = strings.Repeat("ab ", typeset.MaxTextRunes/3) },
		"maximum text nl":  func(p *typeset.Params) { p.Text = strings.Repeat("\n", typeset.MaxTextRunes) },
		"huge on tiny":     func(p *typeset.Params) { p.Size, p.PageWidth, p.PageHeight = 14400, 1, 1 },
		"narrow box words": func(p *typeset.Params) { p.Text, p.WrapWidth = strings.Repeat("word ", 200), 1 },
	}

	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := baseParams("Rīgas ļoti")
			edit(&p)

			placed := place(t, p)

			values := make([]float64, 0, 4+4*len(placed.Lines))
			values = append(values, placed.Bounds.X, placed.Bounds.Y, placed.Bounds.Width, placed.Bounds.Height)

			for _, line := range placed.Lines {
				values = append(values, line.X, line.Baseline, line.Width, line.ExtraSpace)
			}

			for _, value := range values {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("non-finite value in %+v", placed.Bounds)
				}
			}
		})
	}
}

func TestLineSpacingIsBaselineDistanceInFontSizes(t *testing.T) {
	t.Parallel()

	p := baseParams("a\nb")
	p.Size = 10
	single := place(t, p)

	p.LineSpacing = 2
	double := place(t, p)

	lead1 := single.Lines[0].Baseline - single.Lines[1].Baseline
	lead2 := double.Lines[0].Baseline - double.Lines[1].Baseline

	if !near(lead1, 10) || !near(lead2, 2*lead1) {
		t.Errorf("advances %v and %v", lead1, lead2)
	}
}

func TestPlacementIsDeterministicAndShaperReusable(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	font := defaultFont(t)
	params := baseParams("Rīgas ļoti x́ q̄ Ελληνικά Привет")
	params.WrapWidth, params.Align, params.Language = 90, typeset.AlignJustify, "lv"

	first, err := shaper.Place(font, params)
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		again, againErr := shaper.Place(font, params)
		if againErr != nil || lineTexts(again) != lineTexts(first) || again.Bounds != first.Bounds {
			t.Fatalf("not repeatable: %v", againErr)
		}
	}
}

// A Font is shared by goroutines, each with its own Shaper (run with -race).
func TestSharedFontConcurrentShapers(t *testing.T) {
	t.Parallel()

	font := defaultFont(t)
	results := make(chan string, 8)

	for range 8 {
		go func() {
			var shaper typeset.Shaper

			params := baseParams("Rīgas ļoti x́ q̄ Ελληνικά Привет")
			params.WrapWidth, params.Align = 90, typeset.AlignJustify

			var last string

			for range 30 {
				placed, err := shaper.Place(font, params)
				if err != nil {
					results <- err.Error()

					return
				}

				last = lineTexts(placed)
			}

			results <- last
		}()
	}

	first := <-results
	for range 7 {
		if got := <-results; got != first {
			t.Errorf("goroutines disagree: %q vs %q", got, first)
		}
	}
}
