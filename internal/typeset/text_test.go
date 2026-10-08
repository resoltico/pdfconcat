// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/typeset"
)

const (
	controlReason = "control character"
	formatReason  = "format character"
)

func TestRejectedText(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		text string
		want string
	}{
		"arabic":               {"مرحبا", "script Arab"},
		"hebrew":               {"שלום", "script Hebr"},
		"devanagari":           {"हि", "script Deva"},
		"thai":                 {"สวัสดี", "script Thai"},
		"cjk":                  {"\u4f60", "script Hani"},
		"tab":                  {"a\tb", "TAB is not supported"},
		"vertical tab":         {"a\vb", controlReason},
		"nul":                  {"a\x00b", controlReason},
		"nel":                  {"a\u0085b", controlReason},
		"line separator":       {"a\u2028b", "separators"},
		"paragraph sep":        {"a\u2029b", "separators"},
		"zwj":                  {"a\u200db", formatReason},
		"bidi mark":            {"a\u200fb", formatReason},
		"soft hyphen":          {"a\u00adb", formatReason},
		"invalid utf-8":        {"a\xffb", "not valid UTF-8"},
		"missing glyph":        {"ok ☃ no", "no glyph"},
		"private use":          {"a\ue000b", "script Zzzz"},
		"unassigned":           {"a\U000e0080b", "script"},
		"arabic question mark": {"a\u061fb", "right-to-left"},
		"mixed rtl in ltr":     {"abc א def", "script Hebr"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var shaper typeset.Shaper

			placed, err := shaper.Place(defaultFont(t), baseParams(tc.text))

			var textErr *typeset.TextError
			if !errors.As(err, &textErr) || placed != nil {
				t.Fatalf(gotValueFormat, err)
			}

			message := err.Error()
			if !strings.Contains(message, tc.want) || !strings.Contains(message, "supported: left-to-right Latin, Greek and Cyrillic") {
				t.Errorf(messageValueFormat, message)
			}
		})
	}
}

func TestTextErrorPositionsAndMessage(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	// The tab is at rune 3; a run of five Hebrew letters reports one problem with four more.
	_, err := shaper.Place(defaultFont(t), baseParams("abc\tשלום!"))

	var textErr *typeset.TextError
	if !errors.As(err, &textErr) || len(textErr.Problems) != 2 {
		t.Fatalf("%v", err)
	}

	if problem := textErr.Problems[0]; problem.Offset != 3 || problem.Rune != '\t' {
		t.Errorf("first problem %+v", problem)
	}

	if problem := textErr.Problems[1]; problem.Offset != 4 || problem.More != 3 {
		t.Errorf("second problem %+v", problem)
	}

	if !strings.Contains(err.Error(), "U+0009 at rune 3") || !strings.Contains(err.Error(), "(+3 more)") {
		t.Errorf(messageValueFormat, err)
	}
}

func TestSeparatedTextProblemsAreCapped(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	_, err := shaper.Place(defaultFont(t), baseParams(strings.Repeat("\ta", 40)))

	var textErr *typeset.TextError
	if !errors.As(err, &textErr) || len(textErr.Problems) != 8 {
		t.Errorf("problems not capped: %v", err)
	}
}

func TestMissingGlyphOffsetCountsAcrossParagraphs(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	_, err := shaper.Place(defaultFont(t), baseParams("ab\ncd ☃"))

	var textErr *typeset.TextError
	if !errors.As(err, &textErr) || textErr.Problems[0].Offset != 6 {
		t.Errorf("missing glyph offset: %v", err)
	}
}

func TestMissingGlyphsAreCappedAndNeverSubstituted(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	_, err := shaper.Place(defaultFont(t), baseParams(strings.Repeat("☃ ", 20)))

	var textErr *typeset.TextError
	if !errors.As(err, &textErr) || len(textErr.Problems) != 8 {
		t.Fatalf("%v", err)
	}
}

func TestSupportedScriptsAndMarks(t *testing.T) {
	t.Parallel()

	samples := []string{
		"Rīgas ļoti Ēdīgas ķirbis", "ā ļ á", "ā ļ á", "x́ q̄ z̧ t̄́",
		"Ελληνικά άέήίόύώ", "Привет мир", "0123456789 !?.,;:()[]{}", "no break",
		"ĀČĒĢĪĶĻŅŠŪŽ āčēģīķļņšūž",
	}

	for _, text := range samples {
		placed := place(t, baseParams(text))

		var joined strings.Builder
		for _, l := range placed.Lines {
			joined.WriteString(l.Text())
		}

		if joined.String() != text {
			t.Errorf("text changed: %q -> %q", text, joined.String())
		}
	}
}

func TestDecomposedAndPrecomposedAreDistinctGlyphPaths(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{{"ā", "ā"}, {"ļ", "ļ"}}

	for _, pair := range pairs {
		pre := place(t, baseParams(pair[0])).Lines[0].Clusters
		dec := place(t, baseParams(pair[1])).Lines[0].Clusters

		if pre[0].Text != pair[0] || dec[0].Text != pair[1] {
			t.Errorf("cluster text normalized: %q %q", pre[0].Text, dec[0].Text)
		}

		if len(pre[0].Glyphs) != 1 {
			t.Errorf("precomposed %q should shape to one glyph, got %d", pair[0], len(pre[0].Glyphs))
		}
	}
}

func TestMarkIsPositionedByGPOS(t *testing.T) {
	t.Parallel()

	// q with macron has no precomposed glyph, so only mark attachment can place the mark.
	p := baseParams("q̄")
	p.Size = 1000
	cluster := place(t, p).Lines[0].Clusters[0]

	if len(cluster.Glyphs) != 2 {
		t.Fatalf("want base and mark glyphs, got %d", len(cluster.Glyphs))
	}

	mark := cluster.Glyphs[1]
	if mark.XOffset == 0 && mark.YOffset == 0 {
		t.Error("mark has no GPOS offset")
	}

	if mark.Advance != 0 {
		t.Errorf("mark advance %d, want 0", mark.Advance)
	}
}

func TestLanguageSelectsLocalizedForms(t *testing.T) {
	t.Parallel()

	// A language tag must be accepted; Latvian text shapes identically in this font.
	p := baseParams("ģķļņ")
	p.Language = "lv"

	if got := lineTexts(place(t, p)); got != "ģķļņ" {
		t.Errorf("%q", got)
	}
}

func TestInvalidParamErrorMessage(t *testing.T) {
	t.Parallel()

	var shaper typeset.Shaper

	params := baseParams("x")
	params.Size = 0

	_, err := shaper.Place(defaultFont(t), params)
	if err == nil || !strings.Contains(err.Error(), "invalid text parameter size: must be a number from 0.01 to 14400 points") {
		t.Errorf("%v", err)
	}
}
