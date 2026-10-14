// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"testing"
)

func TestVariableWrappingConsumesOversizedAndZeroAdvanceGlyphs(t *testing.T) {
	t.Parallel()
	font := &variableFont{codes: map[rune]byte{'A': 'A', 'B': 'B', 'C': 'C', ' ': ' '}}
	for _, code := range []byte{'A', 'B', 'C', ' '} {
		font.known[code] = true
	}
	font.widths['A'], font.widths['B'], font.widths[' '] = 1, 1, .5
	for _, test := range []struct {
		name, text string
		available  float64
		want       []string
		widths     []float64
	}{
		{name: "one oversized glyph", text: "A", available: 0, want: []string{"A"}, widths: []float64{1}},
		{name: "oversized word", text: "AB", available: 0, want: []string{"A", "B"}, widths: []float64{1, 1}},
		{name: "exact width", text: "AB", available: 24, want: []string{"AB"}, widths: []float64{2}},
		{name: "zero advances at zero width", text: "CC", available: 0, want: []string{"CC"}, widths: []float64{0}},
		{name: "zero then oversized", text: "CA", available: 0, want: []string{"C", "A"}, widths: []float64{0, 1}},
		{name: "fitting suffix after overflow", text: "ABC", available: 12, want: []string{"A", "BC"}, widths: []float64{1, 1}},
		{name: "leading break space", text: " A B", available: 18, want: []string{"", "A", "B"}, widths: []float64{0, 1, 1}},
		{name: "separating spaces", text: "A  B", available: 18, want: []string{"A", "B"}, widths: []float64{1, 1}},
		{name: "trailing spaces after break", text: "A  ", available: 18, want: []string{"A"}, widths: []float64{1}},
		{name: "empty paragraph", text: "", available: 0, want: []string{""}, widths: []float64{0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// An overflow glyph may be inspected again as the next line's first
			// glyph; normal wrapping therefore needs at most two checks per byte.
			ctx := CheckpointContext(t.Context(), t, int64(2*len(test.text)+1))
			lines, err := wrapVariableParagraph(ctx, font, variableAppearanceLine{data: []byte(test.text)}, 12, test.available)
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != len(test.want) {
				t.Fatalf("wrapped%q into%d lines,want%d: %+v", test.text, len(lines), len(test.want), lines)
			}
			for i, line := range lines {
				if string(line.data) != test.want[i] || line.width != test.widths[i] {
					t.Fatalf("line%d=%q width%g,want%q width%g", i, line.data, line.width, test.want[i], test.widths[i])
				}
			}
		})
	}
}

func TestVariableWrappingCancellationPrecedesFirstGlyph(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	font := &variableFont{codes: map[rune]byte{'A': 'A'}}
	font.known['A'] = true
	font.widths['A'] = 1
	if _, err := breakVariableLine(ctx, font, []byte("A"), 0, 12, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("first glyph ignored cancellation: %v", err)
	}
	if lines, err := wrapVariableParagraph(ctx, font, variableAppearanceLine{data: []byte("AB")}, 12, 0); !errors.Is(err, context.Canceled) || lines != nil {
		t.Fatalf("paragraph ignored cancellation: lines=%+v error=%v", lines, err)
	}
}
