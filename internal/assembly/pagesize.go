// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"fmt"
	"strings"
)

// PageDim is the displayed width and height of one page.
type PageDim struct {
	Width, Height Length
}

// PageSize describes the page size of a generated blank: either the size
// inherited from a neighboring source page or an explicit size.
type PageSize struct {
	Inherit bool
	Dim     PageDim
}

// minPageSideLength is the smallest explicit page side accepted.
const minPageSideLength Length = 1

// inheritSizeName is the textual form of PageSize{Inherit: true}.
const inheritSizeName = "inherit"

// Paper sizes in portrait orientation.
const (
	a3WidthMM, a3HeightMM           = 297, 420
	a4WidthMM, a4HeightMM           = 210, 297
	a5WidthMM, a5HeightMM           = 148, 210
	b4WidthMM, b4HeightMM           = 250, 353
	b5WidthMM, b5HeightMM           = 176, 250
	letterWidthIn, letterHeightIn   = 8.5, 11
	legalWidthIn, legalHeightIn     = 8.5, 14
	tabloidWidthIn, tabloidHeightIn = 11, 17
)

// millimetres converts a size in millimetres to a Length.
func millimetres(value float64) Length { return Length(value * pointsPerMillimetre) }

// inches converts a size in inches to a Length.
func inches(value float64) Length { return Length(value * pointsPerInch) }

// namedPageSizes lists the supported paper names in portrait orientation.
func namedPageSizes() map[string]PageDim {
	return map[string]PageDim{
		"a3":      {Width: millimetres(a3WidthMM), Height: millimetres(a3HeightMM)},
		"a4":      {Width: millimetres(a4WidthMM), Height: millimetres(a4HeightMM)},
		"a5":      {Width: millimetres(a5WidthMM), Height: millimetres(a5HeightMM)},
		"b4":      {Width: millimetres(b4WidthMM), Height: millimetres(b4HeightMM)},
		"b5":      {Width: millimetres(b5WidthMM), Height: millimetres(b5HeightMM)},
		"letter":  {Width: inches(letterWidthIn), Height: inches(letterHeightIn)},
		"legal":   {Width: inches(legalWidthIn), Height: inches(legalHeightIn)},
		"tabloid": {Width: inches(tabloidWidthIn), Height: inches(tabloidHeightIn)},
	}
}

// ParsePageSize parses "inherit", a paper name (A3, A4, A5, B4, B5, Letter,
// Legal, Tabloid; portrait), or WIDTHxHEIGHT with an optional shared unit
// such as 210x297mm or 8.5x11in.
func ParsePageSize(text string) (PageSize, error) {
	trimmed := strings.TrimSpace(text)
	if strings.EqualFold(trimmed, inheritSizeName) {
		return PageSize{Inherit: true}, nil
	}

	if dim, found := namedPageSizes()[strings.ToLower(trimmed)]; found {
		return PageSize{Dim: dim}, nil
	}

	widthText, heightText, found := strings.Cut(strings.ToLower(trimmed), "x")
	if !found {
		return PageSize{}, errInvalidPageSize(text)
	}

	unit := trailingUnit(heightText)
	width, widthErr := ParseLength(widthText + unit)

	height, heightErr := ParseLength(heightText)
	if widthErr != nil || heightErr != nil {
		return PageSize{}, errInvalidPageSize(text)
	}

	if width < minPageSideLength || height < minPageSideLength {
		return PageSize{}, fmt.Errorf("page size %q is smaller than %.0f point per side", text, float64(minPageSideLength))
	}

	return PageSize{Dim: PageDim{Width: width, Height: height}}, nil
}

// String formats the size the way ParsePageSize accepts it.
func (s PageSize) String() string {
	if s.Inherit {
		return inheritSizeName
	}

	return fmt.Sprintf("%.2fx%.2fpt", float64(s.Dim.Width), float64(s.Dim.Height))
}

func errInvalidPageSize(text string) error {
	return fmt.Errorf("invalid page size %q: want inherit, a paper name, or WIDTHxHEIGHT[unit]", text)
}

// trailingUnit returns the unit suffix of text, so one unit can apply to both sides.
func trailingUnit(text string) string {
	for _, unit := range lengthUnits() {
		if strings.HasSuffix(strings.TrimSpace(text), unit.suffix) {
			return unit.suffix
		}
	}

	return ""
}
