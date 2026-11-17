// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// Fonts owns one invocation's prepared font values, keyed by effective resource.
// Font values own immutable bytes; acquisition and consumer attribution remain in app.
type Fonts map[packet.Font]*typeset.Font

// ParseFont prepares an acquired font, or the bundled value for the default resource.
func ParseFont(name packet.Font, data []byte) (*typeset.Font, error) {
	if name == (packet.Font{}) {
		return typeset.LoadDefaultFont()
	}
	return typeset.LoadFont(data)
}

// TextPreflight checks geometry-independent text using one authored language.
// Its shaping cache is local to the invocation, independent of filesystem inputs.
type TextPreflight struct {
	shaper   typeset.Shaper
	language typeset.Language
}

// NewTextPreflight adapts the language of an admitted plan.
func NewTextPreflight(plan packet.Plan) *TextPreflight {
	return &TextPreflight{language: typeset.ShapingLanguage(plan.Language())}
}

// Check establishes script and glyph availability before page measurement.
func (p *TextPreflight) Check(font *typeset.Font, text string) error {
	return p.shaper.ValidateText(font, text, p.language)
}

// PrepareFonts collects the actual generated glyphs, including canonical number
// labels, and prepares one shared embedding per font identity. Repeats stay compact.
func PrepareFonts(pages []Page, numbers *PageNumberLayout) (map[typeset.Digest]typeset.EmbeddedFont, error) {
	var placements []*typeset.Placed
	for _, page := range pages {
		for _, block := range page.Blocks {
			placements = append(placements, block.Text)
		}
	}
	if numbers != nil {
		union, err := numbers.Union()
		if err != nil {
			return nil, err
		}
		placements = append(placements, union)
	}
	return typeset.PrepareFonts(placements)
}
