// SPDX-License-Identifier: AGPL-3.0-only
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

// SemanticRole is the authored role of generated meaningful text.
type SemanticRole string

const (
	// RoleParagraph identifies ordinary generated body text.
	RoleParagraph SemanticRole = "P"
	// RoleHeading identifies a generated section or contents heading.
	RoleHeading SemanticRole = "H1"
	// RoleContents identifies a linked table-of-contents entry.
	RoleContents SemanticRole = "TOCI"
)

// TextBlock is one measured, independently colored block.
type TextBlock struct {
	Role  SemanticRole
	Link  *int
	Text  *typeset.Placed
	Color packet.Color
}

// Link is a measured contents row linked to a physical output page.
type Link struct {
	Label      string
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
