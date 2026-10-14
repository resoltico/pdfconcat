// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

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
