// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

func TestOverflowWidthAndPageEdgesKeepInclusiveTolerance(t *testing.T) {
	t.Parallel()
	boundary := 100.0 + 2e-6
	for _, test := range []struct {
		width float64
		wide  bool
	}{{100, false}, {boundary, false}, {math.Nextafter(boundary, math.Inf(1)), true}} {
		failure := &OverflowError{WrapWidth: test.width, PageWidth: 100, Findings: []Finding{{Kind: FindingOutsidePageHorizontal}}}
		if got := failure.FixedWidthExceedsPage(); got != test.wide {
			t.Fatalf("fixed width %.17g classified wide=%v, want %v", test.width, got, test.wide)
		}
	}
	for _, test := range []struct {
		y       float64
		outside bool
	}{{-1e-6, false}, {math.Nextafter(-1e-6, math.Inf(-1)), true}} {
		findings := pageFindings(Rect{Y: test.y, Width: 1, Height: 1}, Params{PageWidth: 100, PageHeight: 100}, "text block")
		if (len(findings) != 0) != test.outside {
			t.Fatalf("lower page edge %.17g findings = %v", test.y, findings)
		}
	}
}

func TestTextProblemAggregationPreservesGapsAndCapacity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text string
		want []Problem
	}{
		{"\u061f", []Problem{{Offset: 0, Rune: '\u061f', Reason: "right-to-left character is not supported"}}},
		{"\t\t\t", []Problem{{Offset: 0, Rune: '\t', More: 2, Reason: "TAB is not supported; use spaces"}}},
		{"\u2028\u2029", []Problem{{Offset: 0, Rune: '\u2028', More: 1, Reason: "line and paragraph separators are not supported; use LF"}}},
		{"\ta\t", []Problem{{Offset: 0, Rune: '\t', Reason: "TAB is not supported; use spaces"}, {Offset: 2, Rune: '\t', Reason: "TAB is not supported; use spaces"}}},
		{"\ta\ta\ta\ta\ta\ta\ta\ta\t\t", []Problem{
			{Offset: 0, Rune: '\t', Reason: "TAB is not supported; use spaces"},
			{Offset: 2, Rune: '\t', Reason: "TAB is not supported; use spaces"},
			{Offset: 4, Rune: '\t', Reason: "TAB is not supported; use spaces"},
			{Offset: 6, Rune: '\t', Reason: "TAB is not supported; use spaces"},
			{Offset: 8, Rune: '\t', Reason: "TAB is not supported; use spaces"},
			{Offset: 10, Rune: '\t', Reason: "TAB is not supported; use spaces"},
			{Offset: 12, Rune: '\t', Reason: "TAB is not supported; use spaces"},
			{Offset: 14, Rune: '\t', Reason: "TAB is not supported; use spaces"},
		}},
	} {
		if got := checkSupported(test.text); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("problems for %q = %+v, want %+v", test.text, got, test.want)
		}
	}
}

func TestPlacementUsesFontMetricsAndGlyphOffsets(t *testing.T) {
	t.Parallel()
	font := &Font{upem: 10, ascent: 7, descent: -2, ink: []Rect{{}, {X: 2, Y: 3, Width: 4, Height: 5}, {X: 1, Y: -2, Width: 3, Height: 4}}}
	placed := &Placed{Font: font, Size: 20}
	placed.layOut(font, Params{Size: 20, PageWidth: 100, PageHeight: 100, LineSpacing: 1.5}, []wrappedLine{
		{clusters: []Cluster{{Text: "A", Glyphs: []Glyph{{ID: 1, Advance: 3}}}}},
		{clusters: []Cluster{{Text: "B", Glyphs: []Glyph{{ID: 2, Advance: 3}}}}},
	})
	if placed.Bounds != (Rect{Y: 52, Width: 6, Height: 48}) || placed.Lines[0].Baseline != 86 || placed.Lines[1].Baseline != 56 {
		t.Fatalf("metric placement = %+v, lines %+v", placed.Bounds, placed.Lines)
	}
	ink := &Placed{Font: font, Size: 20}
	ink.measureLineInk(Line{X: 10, Baseline: 20, Clusters: []Cluster{{Text: "AB", Glyphs: []Glyph{{ID: 1, Advance: 5, XOffset: 1, YOffset: -1}, {ID: 2, Advance: 5}}}}}, 2)
	if ink.InkBounds == nil || *ink.InkBounds != (Rect{X: 16, Y: 16, Width: 12, Height: 18}) {
		t.Fatalf("glyph offset and advance ink = %+v", ink.InkBounds)
	}
}

func TestInkMeasurementAddsJustificationOnlyAfterInteriorSpaces(t *testing.T) {
	t.Parallel()
	font := &Font{ink: []Rect{{}, {Width: 1, Height: 1}}}
	placed := &Placed{Font: font}
	placed.measureLineInk(Line{X: 10, Baseline: 20, ExtraSpace: 4, Clusters: []Cluster{
		{Text: " ", Glyphs: []Glyph{{Advance: 3}}},
		{Text: "A", Glyphs: []Glyph{{ID: 1, Advance: 5}}},
		{Text: " ", Glyphs: []Glyph{{Advance: 3}}},
		{Text: "B", Glyphs: []Glyph{{ID: 1, Advance: 5}}},
		{Text: "C", Glyphs: []Glyph{{ID: 1, Advance: 5}}},
	}}, 1)
	if placed.InkBounds == nil || *placed.InkBounds != (Rect{X: 13, Y: 20, Width: 18, Height: 1}) {
		t.Fatalf("justified glyph positions changed: %+v", placed.InkBounds)
	}
	for _, extent := range []Rect{{Width: 0, Height: 1}, {Width: 1, Height: 0}} {
		font.ink[1] = extent
		empty := &Placed{Font: font}
		empty.includeGlyphInk(Glyph{ID: 1}, 0, 0, 1)
		if empty.InkBounds != nil {
			t.Fatalf("zero-area glyph created visible ink: %+v", empty.InkBounds)
		}
	}
	font.ink[1] = Rect{X: 20, Y: 20, Width: 10, Height: 10}
	overlaid := &Placed{Font: font}
	overlaid.includeGlyphInk(Glyph{ID: 1}, 0, 0, 1)
	overlaid.includeGlyphInk(Glyph{ID: 1, XOffset: -18, YOffset: -18}, 0, 0, 1)
	if overlaid.InkBounds == nil || *overlaid.InkBounds != (Rect{X: 2, Y: 2, Width: 28, Height: 28}) {
		t.Fatalf("later overlaid glyph shrank earlier ink: %+v", overlaid.InkBounds)
	}
}

// The shaping library never produces these results for the supported scripts, so the integrity checks
// that guard against them are exercised directly.
func TestShapingIntegrityChecks(t *testing.T) {
	t.Parallel()

	err := checkRebuilt([]Cluster{{Text: "a"}, {Text: "c"}}, "abc")
	if !errors.Is(err, ErrShapingFailed) || !strings.Contains(err.Error(), "dropped") {
		t.Errorf("dropped text accepted: %v", err)
	}

	err = checkRebuilt([]Cluster{{Text: "ab"}}, "ab")
	if err != nil {
		t.Error(err)
	}

	run := shaping.Input{RunStart: 0, RunEnd: 2, Direction: di.DirectionLTR}
	runes := []rune("ab")

	for name, glyphs := range map[string][]shaping.Glyph{
		"before the run":   {{ClusterIndex: -1, GlyphID: 5}},
		"empty cluster":    {{ClusterIndex: 1, GlyphID: 5}, {ClusterIndex: 1, GlyphID: 6}, {ClusterIndex: 0, GlyphID: 7}},
		"past the run end": {{ClusterIndex: 3, GlyphID: 5}},
	} {
		_, _, runErr := shapeRun(shaping.Output{Glyphs: glyphs}, run, runes, 0)
		if !errors.Is(runErr, ErrShapingFailed) || !strings.Contains(runErr.Error(), "inconsistent cluster") {
			t.Errorf("%s: %v", name, runErr)
		}
	}

	clusters, problems, err := shapeRun(
		shaping.Output{Glyphs: []shaping.Glyph{{ClusterIndex: 0, GlyphID: 0}, {ClusterIndex: 1, GlyphID: 70000}}},
		run,
		runes,
		4,
	)
	if err != nil || len(clusters) != 2 || len(problems) != 2 || problems[1].Offset != 5 {
		t.Errorf("glyph 0 and out-of-range glyphs must be problems: %v %v %v", clusters, problems, err)
	}
}

func TestClusterAdvanceSumsItsGlyphs(t *testing.T) {
	t.Parallel()

	cluster := Cluster{Glyphs: []Glyph{{Advance: 3}, {Advance: 4}, {Advance: -2}}}
	if got := cluster.advance(); got != 5 {
		t.Errorf("advance %d, want 5", got)
	}
}

func TestShapeRunConvertsShaperUnitsToFontUnits(t *testing.T) {
	t.Parallel()

	const (
		unitsPerFontUnit = 64
		wholeAdvance     = 10
		wholeOffset      = 2
		wholeLift        = -3
	)

	run := shaping.Input{RunStart: 0, RunEnd: 2, Direction: di.DirectionLTR}
	glyphs := []shaping.Glyph{
		{
			ClusterIndex: 0, GlyphID: maxGlyphID,
			Advance: fixed.Int26_6(wholeAdvance * unitsPerFontUnit), XOffset: fixed.Int26_6(wholeOffset * unitsPerFontUnit),
			YOffset: fixed.Int26_6(wholeLift * unitsPerFontUnit),
		},
		{ClusterIndex: 1, GlyphID: 7, Advance: -65, XOffset: -65, YOffset: 63},
	}

	clusters, problems, err := shapeRun(shaping.Output{Glyphs: glyphs}, run, []rune("ab"), 0)
	if err != nil || len(problems) != 0 || len(clusters) != 2 {
		t.Fatalf("%v %v %v", clusters, problems, err)
	}

	want := []Glyph{
		{ID: maxGlyphID, Advance: wholeAdvance, XOffset: wholeOffset, YOffset: wholeLift},
		{ID: 7, Advance: -1, XOffset: -1, YOffset: 0},
	}

	for index, glyph := range want {
		if got := clusters[index].Glyphs; len(got) != 1 || got[0] != glyph {
			t.Errorf("cluster %d glyphs %+v, want %+v", index, got, glyph)
		}
	}
}

func TestShapeRunKeepsGlyphsAfterAMissingOne(t *testing.T) {
	t.Parallel()

	run := shaping.Input{RunStart: 0, RunEnd: 1, Direction: di.DirectionLTR}
	glyphs := []shaping.Glyph{{ClusterIndex: 0, GlyphID: 0}, {ClusterIndex: 0, GlyphID: 5}}

	clusters, problems, err := shapeRun(shaping.Output{Glyphs: glyphs}, run, []rune("a"), 0)
	if err != nil || len(problems) != 1 || len(clusters) != 1 || len(clusters[0].Glyphs) != 1 || clusters[0].Glyphs[0].ID != 5 {
		t.Errorf("%v %v %v", clusters, problems, err)
	}
}

func TestShapeRunRejectsAnEmptyLastCluster(t *testing.T) {
	t.Parallel()

	run := shaping.Input{RunStart: 0, RunEnd: 2, Direction: di.DirectionLTR}

	_, _, err := shapeRun(shaping.Output{Glyphs: []shaping.Glyph{{ClusterIndex: 2, GlyphID: 5}}}, run, []rune("ab"), 0)
	if !errors.Is(err, ErrShapingFailed) || !strings.Contains(err.Error(), "inconsistent cluster") {
		t.Errorf("got %v", err)
	}
}

func TestOutlineCheckContinuesPastGlyphsAlreadyChecked(t *testing.T) {
	t.Parallel()

	font, err := LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}

	var shaper Shaper

	state := shaper.fontState(font)
	state.checked[1] = struct{}{}

	// Glyph 1 was checked before; glyph maxGlyphID does not exist in the font, so it has no outline.
	err = state.checkOutlines(font, []Cluster{{Text: "a", Glyphs: []Glyph{{ID: 1}, {ID: maxGlyphID}}}})

	var outline *OutlineError
	if !errors.As(err, &outline) || outline.Glyph != maxGlyphID {
		t.Errorf("got %v", err)
	}
}

func TestNominalTextValidationUsesKnownOutlinesWithoutShaping(t *testing.T) {
	t.Parallel()
	font, err := LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	var shaper Shaper
	state := shaper.fontState(font)
	for range 2 {
		missing, err := state.checkNominalParagraph(font, "AB")
		if err != nil || missing {
			t.Fatalf("intact nominal glyphs required fallback shaping: missing=%v, %v", missing, err)
		}
	}
}

func TestShapingRejectsUnsupportedRunsAtTheirParagraphOffsets(t *testing.T) {
	t.Parallel()
	font, err := LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	for _, paragraph := range []string{"abc \u061f", "abc \u4f60"} {
		var shaper Shaper
		_, err := shaper.shapeParagraph(font, paragraph, 13, "")
		var unsupported *TextError
		if !errors.As(err, &unsupported) || len(unsupported.Problems) != 1 || unsupported.Problems[0].Offset != 17 || !strings.Contains(unsupported.Problems[0].Reason, "unsupported-script run") {
			t.Fatalf("unsupported shaped run lost its cause or absolute rune position: %q, %v", paragraph, err)
		}
	}
}

func TestWordWrappingIncludesTheExactRoundingAllowance(t *testing.T) {
	t.Parallel()
	clusters := []Cluster{{Text: "A", Glyphs: []Glyph{{Advance: 1}}}, {Text: " ", Glyphs: []Glyph{{Advance: 1}}}, {Text: "B", Glyphs: []Glyph{{Advance: 1}}}}
	// Three font units fit at this limit only because of the documented rounding allowance.
	allowance := 1.0 + 1e-9
	boundary := 3.0 / allowance
	if boundary*allowance != 3 {
		t.Fatal("wrapping boundary fixture must exactly fill three font units after rounding")
	}
	for _, test := range []struct {
		limit float64
		lines int
	}{
		{boundary, 1}, {math.Nextafter(math.Nextafter(boundary, math.Inf(-1)), math.Inf(-1)), 2}, {3, 1},
	} {
		lines := breakLines(clusters, test.limit)
		if len(lines) != test.lines {
			t.Fatalf("wrap at %.17g produced %d lines, want %d", test.limit, len(lines), test.lines)
		}
		if test.lines == 2 && (len(lines[0]) != 1 || len(lines[1]) != 1 || lines[0][0].Text != "A" || lines[1][0].Text != "B") {
			t.Fatalf("wrap consumed text or retained break space: %v", lines)
		}
	}
}
