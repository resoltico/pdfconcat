// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine_test

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

const (
	pageWidth  = 600.0
	pageHeight = 800.0
)

// textRun is one positioned line recovered from a rendered content stream.
type textRun struct {
	x, y        float64
	wordSpacing float64
	text        string
}

var runPattern = regexp.MustCompile(`(?m)^([-\d.]+) Tw\n1 0 0 1 ([-\d.]+) ([-\d.]+) Tm\n\((.*)\) Tj$`)

func render(t *testing.T, style assembly.BlankStyle) (string, []textRun) {
	t.Helper()

	spec, err := style.Resolve(assembly.PageDim{Width: pageWidth, Height: pageHeight})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	pdf, err := pdfengine.RenderBlank(context.Background(), spec)
	if err != nil {
		t.Fatalf("renderBlank() error = %v", err)
	}

	text := string(pdf)
	start := strings.Index(text, "stream\n") + len("stream\n")

	content := text[start:strings.Index(text, "\nendstream")]

	matches := runPattern.FindAllStringSubmatch(content, -1)
	runs := make([]textRun, 0, len(matches))

	for _, match := range matches {
		runs = append(
			runs,
			textRun{x: parseFloat(t, match[2]), y: parseFloat(t, match[3]), wordSpacing: parseFloat(t, match[1]), text: match[4]},
		)
	}

	return content, runs
}

func parseFloat(t *testing.T, text string) float64 {
	t.Helper()

	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}

	return value
}

func textStyle(mutate func(*assembly.TextStyle)) assembly.BlankStyle {
	style := assembly.TextStyle{
		Value:  assembly.Some("Hello"),
		Font:   assembly.Some(assembly.Font("Courier")), // 600 units per glyph: easy arithmetic.
		Size:   assembly.Some(assembly.Length(10)),      // Hello = 5 * 6 = 30 points wide.
		Width:  assembly.Some(assembly.Length(200)),
		Anchor: assembly.Some(assembly.AnchorBottomLeft),
		Align:  assembly.Some(assembly.AlignLeft),
	}
	mutate(&style)

	return assembly.BlankStyle{Text: style}
}

func TestRenderOmitsTextOperatorsForBlankWithoutText(t *testing.T) {
	t.Parallel()

	content, runs := render(t, assembly.BlankStyle{})
	if content != "" || len(runs) != 0 {
		t.Fatalf("plain blank content = %q", content)
	}
}

func TestRenderFillsBackground(t *testing.T) {
	t.Parallel()

	content, _ := render(t, assembly.BlankStyle{Background: assembly.Some(assembly.Color{R: 255, G: 0, B: 51})})
	if !strings.Contains(content, "1 0 0.2 rg\n0 0 600 800 re\nf") {
		t.Fatalf("content = %q", content)
	}
}

func TestRenderPlacesTextByAnchorAlignmentAndOffset(t *testing.T) {
	t.Parallel()

	// Courier 10pt, leading 1.2: line box 12pt, glyph box (629+157)/100 = 7.86pt, ascent 6.29pt.
	// Baseline above the line-box bottom edge: (12-7.86)/2 + 1.57 (descent) = 3.64.
	const baselineAboveBox = 3.64

	tests := []struct {
		name   string
		mutate func(*assembly.TextStyle)
		wantX  float64
		wantY  float64
	}{
		{"bottom-left", func(*assembly.TextStyle) {}, 0, baselineAboveBox},
		{"bottom-left with offset", func(s *assembly.TextStyle) {
			s.X, s.Y = assembly.Some(assembly.Length(15)), assembly.Some(assembly.Length(20))
		}, 15, 20 + baselineAboveBox},
		{"bottom-right box with right align", func(s *assembly.TextStyle) {
			s.Anchor, s.Align = assembly.Some(assembly.AnchorBottomRight), assembly.Some(assembly.AlignRight)
		}, pageWidth - 30, baselineAboveBox},
		{"top centers box then centers line", func(s *assembly.TextStyle) {
			s.Anchor, s.Align = assembly.Some(assembly.AnchorTop), assembly.Some(assembly.AlignCenter)
		}, (pageWidth-200)/2 + (200-30)/2, pageHeight - 12 + baselineAboveBox},
		{
			"center",
			func(s *assembly.TextStyle) { s.Anchor = assembly.Some(assembly.AnchorCenter) },
			0 + (pageWidth-200)/2,
			pageHeight/2 - 6 + baselineAboveBox,
		},
		{
			"left middle",
			func(s *assembly.TextStyle) { s.Anchor = assembly.Some(assembly.AnchorLeft) },
			0,
			pageHeight/2 - 6 + baselineAboveBox,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, runs := render(t, textStyle(test.mutate))
			if len(runs) != 1 {
				t.Fatalf("runs = %+v", runs)
			}

			if diff := runs[0].x - test.wantX; diff < -0.01 || diff > 0.01 {
				t.Errorf("x = %v, want %v", runs[0].x, test.wantX)
			}

			if diff := runs[0].y - test.wantY; diff < -0.01 || diff > 0.01 {
				t.Errorf("y = %v, want %v", runs[0].y, test.wantY)
			}
		})
	}
}

func TestRenderWrapsAndJustifies(t *testing.T) {
	t.Parallel()

	// Each Courier 10pt glyph is 6pt wide; a 60pt box holds 10 glyphs.
	style := textStyle(func(s *assembly.TextStyle) {
		s.Value = assembly.Some("aaa bbb ccc\ndddddddddddddd")
		s.Width = assembly.Some(assembly.Length(60))
		s.Align = assembly.Some(assembly.AlignJustify)
		s.Anchor = assembly.Some(assembly.AnchorTopLeft)
	})
	_, runs := render(t, style)

	wantLines := []string{"aaa bbb", "ccc", "dddddddddddddd"}
	if len(runs) != len(wantLines) {
		t.Fatalf("runs = %+v, want %d lines", runs, len(wantLines))
	}

	for index, want := range wantLines {
		if runs[index].text != want {
			t.Errorf("line %d = %q, want %q", index, runs[index].text, want)
		}
	}
	// "aaa bbb" is 42pt in a 60pt box with one gap: 18pt of extra word spacing, but the
	// last line of a paragraph and an unbreakable word stay unjustified.
	if runs[0].wordSpacing != 18 || runs[1].wordSpacing != 0 || runs[2].wordSpacing != 0 {
		t.Errorf("word spacing = %v, %v, %v", runs[0].wordSpacing, runs[1].wordSpacing, runs[2].wordSpacing)
	}

	if runs[0].y-runs[1].y != 12 {
		t.Errorf("line pitch = %v, want 12", runs[0].y-runs[1].y)
	}
}

func TestRenderEscapesAndEncodesText(t *testing.T) {
	t.Parallel()

	_, runs := render(t, textStyle(func(s *assembly.TextStyle) { s.Value = assembly.Some(`a(b)c\d é–€`) }))
	if len(runs) != 1 || runs[0].text != `a\(b\)c\\d \351\226\200` {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestRenderRejectsUnencodableText(t *testing.T) {
	t.Parallel()

	// U+0100 (Latvian A with macron), Japanese, and a control character are all outside Windows-1252.
	for _, value := range []string{"\u0100", "\u65e5\u672c", "\x01"} {
		style := textStyle(func(s *assembly.TextStyle) { s.Value = assembly.Some("ok " + value) })

		spec, resolveErr := style.Resolve(assembly.PageDim{Width: pageWidth, Height: pageHeight})
		if resolveErr != nil {
			t.Fatalf("Resolve() error = %v", resolveErr)
		}

		_, err := pdfengine.RenderBlank(context.Background(), spec)
		if err == nil {
			t.Errorf("renderBlank accepted %q", value)
		}
	}
}
