// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// fitTolerance absorbs floating-point rounding when a word exactly fills the box.
const fitTolerance = 1e-9

// breakLines wraps one paragraph greedily at U+0020 only. limit is the available width in font
// units, 0 meaning no wrapping. Spaces at a wrap point are consumed by the break; leading spaces of
// the paragraph and runs of spaces inside a line are kept. A word wider than the limit gets a line of
// its own; the caller reports it. An empty paragraph yields one empty line.
func breakLines(clusters []Cluster, limit float64) [][]Cluster {
	if len(clusters) == 0 {
		return [][]Cluster{nil}
	}

	if limit <= 0 {
		return [][]Cluster{clusters}
	}

	var (
		lines     [][]Cluster
		lineStart int
		lineWidth float64
		hasWord   bool
		wordEnd   int
	)

	for first := range clusters {
		if first < wordEnd {
			continue
		}
		if clusters[first].IsSpace() {
			continue
		}

		// first is a non-space cluster; consuming it guarantees progress.
		wordEnd = first + 1
		wordWidth := float64(clusters[first].advance())
		for wordEnd < len(clusters) && !clusters[wordEnd].IsSpace() {
			wordWidth += float64(clusters[wordEnd].advance())
			wordEnd++
		}

		gapStart, gap := first, 0.0
		for gapStart > lineStart && clusters[gapStart-1].IsSpace() {
			gapStart--
			gap += float64(clusters[gapStart].advance())
		}

		switch {
		case hasWord && lineWidth+gap+wordWidth > limit*(1+fitTolerance):
			lines = append(lines, clusters[lineStart:gapStart])
			lineStart, lineWidth = first, wordWidth
		default:
			lineWidth += gap + wordWidth
		}

		hasWord = true
	}

	return append(lines, clusters[lineStart:])
}

// emergencyLines preserves ordinary wrapping and only splits isolated oversized tokens.
// It does not repack following words into the final fragment. Indentation stays on
// the first fragment and trailing spaces stay on the last; neither is discarded.
func emergencyLines(lines [][]Cluster, limit float64) [][]Cluster {
	var out [][]Cluster
	for _, line := range lines {
		first, last := 0, len(line)
		for first < last && line[first].IsSpace() {
			first++
		}
		for last > first && line[last-1].IsSpace() {
			last--
		}
		width, spaces := lineWidth(line[first:last])
		if spaces > 0 || width <= limit*(1+fitTolerance) {
			out = append(out, line)
			continue
		}
		out = append(out, splitToken(line, first, last, limit)...)
	}
	return out
}

// splitToken groups existing shaping clusters into indivisible units. A unit ends
// only at an extended-grapheme boundary whose next cluster is safe to break before.
// Keeping the shaped units avoids changing ligatures or context-dependent glyphs.
func splitToken(line []Cluster, first, last int, limit float64) [][]Cluster {
	var text strings.Builder
	for _, cluster := range line {
		text.WriteString(cluster.Text)
	}
	boundaries := make([]bool, text.Len()+1)
	iter := graphemes.FromString(text.String())
	for iter.Next() {
		boundaries[iter.End()] = true
	}
	var out [][]Cluster
	start, unitStart, offset := 0, first, 0
	width := 0.0
	for _, cluster := range line[:first] {
		width += float64(cluster.advance())
		offset += len(cluster.Text)
	}
	unitWidth := 0.0
	for end := first + 1; end <= last; end++ {
		cluster := line[end-1]
		offset += len(cluster.Text)
		unitWidth += float64(cluster.advance())
		if end < last && (!boundaries[offset] || line[end].unsafeToBreak) {
			continue
		}
		if unitStart > first && width+unitWidth > limit*(1+fitTolerance) {
			out = append(out, line[start:unitStart])
			start, width = unitStart, 0
		}
		width += unitWidth
		unitStart, unitWidth = end, 0
	}
	return append(out, line[start:])
}
