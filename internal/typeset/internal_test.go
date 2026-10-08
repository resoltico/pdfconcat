// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

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
