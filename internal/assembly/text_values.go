// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"fmt"
	"strings"
)

// Font is the name of one of the PDF standard text fonts, which every PDF
// viewer provides without embedding.
type Font string

// fonts lists the supported standard fonts.
func fonts() []Font {
	return []Font{
		"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
		"Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic",
		"Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique",
	}
}

// ParseFont resolves a standard font name, ignoring case.
func ParseFont(name string) (Font, error) {
	for _, font := range fonts() {
		if strings.EqualFold(string(font), name) {
			return font, nil
		}
	}

	names := make([]string, 0, len(fonts()))
	for _, font := range fonts() {
		names = append(names, string(font))
	}

	return "", fmt.Errorf("unknown font %q: want one of %s", name, strings.Join(names, ", "))
}

// Anchor selects the point on the page that a text block is attached to.
type Anchor uint8

// The nine anchor points of a page.
const (
	AnchorTopLeft Anchor = iota + 1
	AnchorTop
	AnchorTopRight
	AnchorLeft
	AnchorCenter
	AnchorRight
	AnchorBottomLeft
	AnchorBottom
	AnchorBottomRight
)

// anchors lists all anchors in name order.
func anchors() []Anchor {
	return []Anchor{
		AnchorTopLeft, AnchorTop, AnchorTopRight,
		AnchorLeft, AnchorCenter, AnchorRight,
		AnchorBottomLeft, AnchorBottom, AnchorBottomRight,
	}
}

// String returns the anchor's configuration name.
func (a Anchor) String() string {
	switch a {
	case AnchorTopLeft:
		return "top-left"
	case AnchorTop:
		return "top"
	case AnchorTopRight:
		return "top-right"
	case AnchorLeft:
		return "left"
	case AnchorCenter:
		return "center"
	case AnchorRight:
		return "right"
	case AnchorBottomLeft:
		return "bottom-left"
	case AnchorBottom:
		return "bottom"
	case AnchorBottomRight:
		return "bottom-right"
	default:
		return fmt.Sprintf("anchor(%d)", uint8(a))
	}
}

// ParseAnchor resolves an anchor name, ignoring case.
func ParseAnchor(name string) (Anchor, error) {
	names := make([]string, 0, len(anchors()))
	for _, anchor := range anchors() {
		if strings.EqualFold(anchor.String(), name) {
			return anchor, nil
		}

		names = append(names, anchor.String())
	}

	return 0, fmt.Errorf("unknown anchor %q: want one of %s", name, strings.Join(names, ", "))
}

// anchorGridColumns is the width of the 3x3 anchor grid, laid out row by row from top-left.
const anchorGridColumns = 3

// Horizontal returns the horizontal component of the anchor, which is its column in the grid.
func (a Anchor) Horizontal() HorizontalPlacement {
	return HorizontalPlacement((a-AnchorTopLeft)%anchorGridColumns) + PlaceLeft
}

// Vertical returns the vertical component of the anchor, which is its row in the grid.
func (a Anchor) Vertical() VerticalPlacement {
	return VerticalPlacement((a-AnchorTopLeft)/anchorGridColumns) + PlaceTop
}

// HorizontalPlacement is where a text block sits relative to its anchor's x coordinate.
type HorizontalPlacement uint8

// Horizontal placements.
const (
	PlaceLeft HorizontalPlacement = iota + 1
	PlaceCenter
	PlaceRight
)

// VerticalPlacement is where a text block sits relative to its anchor's y coordinate.
type VerticalPlacement uint8

// Vertical placements.
const (
	PlaceTop VerticalPlacement = iota + 1
	PlaceMiddle
	PlaceBottom
)

// TextAlign is the justification of lines within a text block.
type TextAlign uint8

// Text alignments.
const (
	AlignLeft TextAlign = iota + 1
	AlignCenter
	AlignRight
	AlignJustify
)

// String returns the alignment's configuration name.
func (a TextAlign) String() string {
	switch a {
	case AlignLeft:
		return "left"
	case AlignCenter:
		return "center"
	case AlignRight:
		return "right"
	case AlignJustify:
		return "justify"
	default:
		return fmt.Sprintf("align(%d)", uint8(a))
	}
}

// ParseTextAlign resolves an alignment name, ignoring case.
func ParseTextAlign(name string) (TextAlign, error) {
	for _, align := range []TextAlign{AlignLeft, AlignCenter, AlignRight, AlignJustify} {
		if strings.EqualFold(align.String(), name) {
			return align, nil
		}
	}

	return 0, fmt.Errorf("unknown alignment %q: want left, center, right, or justify", name)
}
