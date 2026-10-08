// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import (
	"fmt"
	"strings"
)

type (
	// Anchor selects the point on the page that a text block is attached to.
	Anchor uint8

	// HorizontalPlacement is where a text block sits relative to its anchor's x coordinate.
	HorizontalPlacement uint8

	// VerticalPlacement is where a text block sits relative to its anchor's y coordinate.
	VerticalPlacement uint8
)

const (
	// AnchorTopLeft and the eight anchors after it are the nine anchor points of a page, in grid order.
	AnchorTopLeft Anchor = 1
	// AnchorTop is the top edge, centered.
	AnchorTop Anchor = 2
	// AnchorTopRight is the top right corner.
	AnchorTopRight Anchor = 3
	// AnchorLeft is the left edge, centered.
	AnchorLeft Anchor = 4
	// AnchorCenter is the middle of the page.
	AnchorCenter Anchor = 5
	// AnchorRight is the right edge, centered.
	AnchorRight Anchor = 6
	// AnchorBottomLeft is the bottom left corner.
	AnchorBottomLeft Anchor = 7
	// AnchorBottom is the bottom edge, centered.
	AnchorBottom Anchor = 8
	// AnchorBottomRight is the bottom right corner.
	AnchorBottomRight Anchor = 9

	// PlaceLeft puts the left edge of the block at the anchor.
	PlaceLeft HorizontalPlacement = 1
	// PlaceCenter centers the block on the anchor.
	PlaceCenter HorizontalPlacement = 2
	// PlaceRight puts the right edge of the block at the anchor.
	PlaceRight HorizontalPlacement = 3

	// PlaceTop hangs the block below the anchor.
	PlaceTop VerticalPlacement = 1
	// PlaceMiddle centers the block on the anchor.
	PlaceMiddle VerticalPlacement = 2
	// PlaceBottom stands the block above the anchor.
	PlaceBottom VerticalPlacement = 3

	// anchorGridColumns is the width of the 3x3 anchor grid, laid out row by row from top-left.
	anchorGridColumns = 3
)

// anchors lists all anchors in name order.
func anchors() []Anchor {
	return []Anchor{
		AnchorTopLeft, AnchorTop, AnchorTopRight,
		AnchorLeft, AnchorCenter, AnchorRight,
		AnchorBottomLeft, AnchorBottom, AnchorBottomRight,
	}
}

// AnchorNames lists the anchor names in grid order.
func AnchorNames() []string {
	names := make([]string, 0, len(anchors()))
	for _, anchor := range anchors() {
		names = append(names, anchor.String())
	}

	return names
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

// ParseAnchor resolves an anchor name (exact lower case).
func ParseAnchor(name string) (Anchor, error) {
	for _, anchor := range anchors() {
		if anchor.String() == name {
			return anchor, nil
		}
	}

	return 0, fmt.Errorf("%w %q for anchor: want one of %s", ErrUnknownName, name, strings.Join(AnchorNames(), ", "))
}

// Horizontal returns the horizontal component of the anchor, which is its column in the grid.
func (a Anchor) Horizontal() HorizontalPlacement {
	return HorizontalPlacement((a-AnchorTopLeft)%anchorGridColumns) + PlaceLeft
}

// Vertical returns the vertical component of the anchor, which is its row in the grid.
func (a Anchor) Vertical() VerticalPlacement {
	return VerticalPlacement((a-AnchorTopLeft)/anchorGridColumns) + PlaceTop
}
