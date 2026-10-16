// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package typeset

import (
	"fmt"
	"math"
)

// Digits positions independently shaped Arabic digits in fixed measured cells.
// Bounds are physical points relative to the baseline, without a canvas limit.
type Digits struct {
	Font                           *Font
	Size, CellWidth                float64
	Bottom, Top, InkBottom, InkTop float64
	clusters                       [10]Cluster
}

// NewDigits shapes each digit once, retaining logical and painted cell bounds.
func NewDigits(font *Font, size float64) (*Digits, error) {
	if font == nil || !(size >= MinFontSize && size <= MaxFontSize) {
		return nil, &InvalidParamError{Field: "size", Message: "invalid digit font or size"}
	}
	result := &Digits{Font: font, Size: size}
	var shaper Shaper
	left, right := 0.0, 0.0
	inkBottom, inkTop := math.Inf(1), math.Inf(-1)
	for index := range 10 {
		text := string(rune('0' + index))
		clusters, err := shaper.shapeParagraph(font, text, 0, "")
		if err != nil {
			return nil, err
		}
		joined := Cluster{Text: text}
		for _, cluster := range clusters {
			joined.Glyphs = append(joined.Glyphs, cluster.Glyphs...)
		}
		result.clusters[index] = joined
		p := &Placed{Font: font, Size: float64(font.upem), Lines: []Line{{Clusters: []Cluster{joined}}}}
		p.measureInk()
		right = math.Max(right, float64(joined.advance()))
		if p.InkBounds != nil {
			b := p.InkBounds
			left = math.Min(left, b.X)
			right = math.Max(right, b.X+b.Width)
			inkBottom = math.Min(inkBottom, b.Y)
			inkTop = math.Max(inkTop, b.Y+b.Height)
		}
	}
	if math.IsInf(inkBottom, 0) {
		return nil, fmt.Errorf("%w: digit font has no visible ink", ErrUnsupportedFont)
	}
	shift, cell := int(math.Ceil(-left)), int(math.Ceil(right-left))
	for index := range result.clusters {
		glyphs := result.clusters[index].Glyphs
		for i := range glyphs {
			glyphs[i].XOffset += shift
		}
		glyphs[len(glyphs)-1].Advance += cell - result.clusters[index].advance()
	}
	scale := size / float64(font.upem)
	result.CellWidth = float64(cell) * scale
	result.InkBottom = inkBottom * scale
	result.InkTop = inkTop * scale
	result.Bottom = math.Min(font.descent, inkBottom) * scale
	result.Top = math.Max(font.ascent, inkTop) * scale
	return result, nil
}

// Place right-aligns a nonempty decimal string at right with the given baseline.
// One cluster preserves contiguous extraction independently of cell positioning.
func (d *Digits) Place(text string, right, baseline float64) (*Placed, error) {
	if text == "" || math.IsNaN(right) || math.IsInf(right, 0) || math.IsNaN(baseline) || math.IsInf(baseline, 0) {
		return nil, fmt.Errorf("%w: invalid numeric placement", ErrShapingFailed)
	}
	cluster := Cluster{Text: text}
	for _, digit := range []byte(text) {
		if digit < '0' || digit > '9' {
			return nil, fmt.Errorf("%w: expected Arabic decimal digits", ErrShapingFailed)
		}
		cluster.Glyphs = append(cluster.Glyphs, d.clusters[digit-'0'].Glyphs...)
	}
	width := float64(len(text)) * d.CellWidth
	placed := &Placed{Font: d.Font, Size: d.Size, Bounds: Rect{X: right - width, Y: baseline + d.Bottom, Width: width, Height: d.Top - d.Bottom}, Lines: []Line{{X: right - width, Baseline: baseline, Width: width, Clusters: []Cluster{cluster}}}}
	placed.measureInk()
	return placed, nil
}
