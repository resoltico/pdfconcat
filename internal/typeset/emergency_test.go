// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/harfbuzz"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

func TestEmergencyWrappingBoundariesAndWhitespace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		texts  []string
		unsafe int
		limit  float64
		want   []string
	}{
		{"grapheme-across-shaping-clusters", []string{"a", "\u0304", "b", "c"}, -1, 2, []string{"a\u0304", "bc"}},
		{"shaping-context", []string{"A", "V", "1", "2"}, 1, 1, []string{"AV", "1", "2"}},
		{"ligature", []string{"ffi", "1", "2"}, -1, 1, []string{"ffi", "1", "2"}},
		{"indent-and-trailing-spaces", []string{" ", "1", "2", "3", "4", " "}, -1, 2, []string{" 1", "23", "4 "}},
		{"no-repacking", []string{"1", "2", "3", "4", "5", " ", "6"}, -1, 3, []string{"123", "45", "6"}},
		{"nbsp-is-preserved", []string{"1", "\u00a0", "2"}, -1, 2, []string{"1\u00a0", "2"}},
		{"indent-only-overflow", []string{" ", " ", "1"}, -1, 1, []string{"  1"}},
		{"exact-fit", []string{"1", "2", "3", "4"}, -1, 2, []string{"12", "34"}},
		{"just-below-fit", []string{"1", "2", "3", "4"}, -1, 2 - 1e-7, []string{"1", "2", "3", "4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clusters := make([]Cluster, len(tc.texts))
			for i, text := range tc.texts {
				clusters[i] = Cluster{Text: text, Glyphs: []Glyph{{Advance: 1}}, unsafeToBreak: i == tc.unsafe}
			}
			var got []string
			for _, line := range emergencyLines(breakLines(clusters, tc.limit), tc.limit) {
				got = append(got, (Line{Clusters: line}).Text())
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShapeRunRetainsUnsafeBoundary(t *testing.T) {
	t.Parallel()
	output := shaping.Output{Glyphs: []shaping.Glyph{
		{GlyphID: 1, ClusterIndex: 0, Advance: fixed.I(1)},
		{GlyphID: 2, ClusterIndex: 1, Advance: fixed.I(1), Mask: harfbuzz.GlyphUnsafeToBreak},
	}}
	clusters, problems, err := shapeRun(output, shaping.Input{RunStart: 0, RunEnd: 2, Direction: di.DirectionLTR}, []rune("AV"), 0)
	if err != nil || len(problems) != 0 || len(clusters) != 2 || clusters[0].unsafeToBreak || !clusters[1].unsafeToBreak {
		t.Fatalf("unsafe boundary lost: %+v, %+v, %v", clusters, problems, err)
	}
}

func TestEmergencyPlacementPreservesUnicodeAndRefusesExhaustion(t *testing.T) {
	t.Parallel()
	font, err := LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	params := Params{Text: strings.Repeat("1", 20), Size: 11, PageWidth: 300, PageHeight: 500, WrapWidth: 20, LineSpacing: 1.2, Anchor: AnchorTopLeft}
	var shaper Shaper
	_, err = shaper.Place(font, params)
	var overflow *OverflowError
	if !errors.As(err, &overflow) {
		t.Fatalf("default must refuse: %v", err)
	}
	params.Wrap = WrapEmergency
	placed, err := shaper.Place(font, params)
	if err != nil {
		t.Fatal(err)
	}
	var rebuilt strings.Builder
	for _, line := range placed.Lines {
		rebuilt.WriteString(line.Text())
		if line.Width > 20 {
			t.Fatal("fragment wider than box")
		}
	}
	if rebuilt.String() != params.Text || len(placed.Lines) < 2 {
		t.Fatal("split lost text")
	}
	params.PageHeight = 20
	_, err = shaper.Place(font, params)
	if !errors.As(err, &overflow) {
		t.Fatalf("vertical exhaustion must refuse: %v", err)
	}
	params.Overflow = OverflowAllow
	placed, err = shaper.Place(font, params)
	if err != nil || len(placed.Findings) == 0 {
		t.Fatal("allow must retain overflow findings")
	}
	params.Wrap = WrapPolicy(99)
	_, err = shaper.Place(font, params)
	var invalid *InvalidParamError
	if !errors.As(err, &invalid) || invalid.Field != "wrap" {
		t.Fatalf("invalid policy accepted: %v", err)
	}
}

func TestEmergencyRealShapingKeepsKerningAndLigaturesTogether(t *testing.T) {
	t.Parallel()
	font, err := LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	var shaper Shaper
	clusters, err := shaper.shapeParagraph(font, "AVffi111", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	foundUnsafe, foundLigature := false, false
	for _, c := range clusters {
		foundUnsafe = foundUnsafe || c.unsafeToBreak
		foundLigature = foundLigature || c.Text == "ffi"
	}
	if !foundUnsafe || !foundLigature {
		t.Fatalf("fixture lacks real kerning/ligature: %+v", clusters)
	}
	var rebuilt strings.Builder
	for _, line := range emergencyLines(breakLines(clusters, 1), 1) {
		if len(line) > 0 && line[0].unsafeToBreak {
			t.Fatal("split at unsafe shaping boundary")
		}
		rebuilt.WriteString((Line{Clusters: line}).Text())
	}
	if rebuilt.String() != "AVffi111" {
		t.Fatal("lost shaped text")
	}
}

func TestEmergencyNormalLayoutAndIndivisibleFailures(t *testing.T) {
	t.Parallel()
	font, err := LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	params := Params{Text: "  ordinary words wrap here  ", Size: 11, PageWidth: 300, PageHeight: 500, WrapWidth: 80, LineSpacing: 1.2, Anchor: AnchorTopLeft, Align: AlignJustify}
	var shaper Shaper
	word, err := shaper.Place(font, params)
	if err != nil {
		t.Fatal(err)
	}
	params.Wrap = WrapEmergency
	emergency, err := shaper.Place(font, params)
	if err != nil || !reflect.DeepEqual(word, emergency) {
		t.Fatalf("ordinary layout changed: %v", err)
	}
	params.Text, params.WrapWidth = "ffi", 1
	placed, err := shaper.Place(font, params)
	var overflow *OverflowError
	if !errors.As(err, &overflow) || len(placed.Lines) != 1 || placed.Lines[0].Text() != "ffi" {
		t.Fatalf("indivisible ligature must remain oversized: %+v, %v", placed, err)
	}
	params.Text, params.WrapWidth = strings.Repeat("a\u0304", 12), 20
	placed, err = shaper.Place(font, params)
	if err != nil {
		t.Fatal(err)
	}
	var rebuilt strings.Builder
	for _, line := range placed.Lines {
		if strings.HasPrefix(line.Text(), "\u0304") {
			t.Fatal("combining mark detached from base")
		}
		rebuilt.WriteString(line.Text())
	}
	if rebuilt.String() != params.Text || len(placed.Lines) < 2 {
		t.Fatal("decomposed Unicode lost or did not wrap")
	}
	params.WrapWidth = 0
	placed, err = shaper.Place(font, params)
	if err != nil || len(placed.Lines) != 1 {
		t.Fatalf("zero width must retain no-wrapping policy: %v", err)
	}
}
