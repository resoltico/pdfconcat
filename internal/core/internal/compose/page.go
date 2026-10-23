// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

// Package compose lays out generated text, contents and physical page numbers.
package compose

import (
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

// Page is a generated canvas with measured text blocks and contents links.
type Page struct {
	Width, Height float64
	Background    *packet.Color
	Blocks        []TextBlock
	Links         []Link
}

// TextBlock is one measured, independently colored block.
type TextBlock struct {
	Text  *typeset.Placed
	Color packet.Color
}

// Link is a measured contents row linked to a physical output page.
type Link struct {
	Rect       typeset.Rect
	TargetPage int64
}

// TextFacts records the measured result of one page item's text.
type TextFacts struct {
	Font      string
	FontSize  float64
	Bounds    [4]float64
	InkBounds *[4]float64
	Overflow  bool
}
