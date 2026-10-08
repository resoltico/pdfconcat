// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package assembly

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type (
	// PageDim is the displayed width and height of one page, in points.
	PageDim struct {
		Width, Height Length
	}

	// PageSize describes the page size of a generated blank: either the size inherited from a
	// neighboring source page or an explicit size.
	PageSize struct {
		Dim     PageDim
		Inherit bool
	}

	// namedPageSize is a paper name with its portrait dimensions in a unit.
	namedPageSize struct {
		name, width, height, unit string
	}
)

const (
	// MinPageSide is the smallest page side of a generated page.
	MinPageSide Length = 1

	// MaxPageSide is the largest page side of a generated page (the PDF implementation limit of 200 inches).
	MaxPageSide = MaxLength

	// inheritSizeName is the textual form of PageSize{Inherit: true}.
	inheritSizeName = "inherit"
)

// NewPageDim checks that both sides are finite and between MinPageSide and MaxPageSide.
func NewPageDim(width, height Length) (PageDim, error) {
	dim := PageDim{Width: width, Height: height}

	err := dim.Validate()
	if err != nil {
		return PageDim{}, err
	}

	return dim, nil
}

// Validate checks that both sides are finite and between MinPageSide and MaxPageSide.
func (d PageDim) Validate() error {
	for _, side := range []Length{d.Width, d.Height} {
		value := float64(side)
		if math.IsNaN(value) || math.IsInf(value, 0) || side < MinPageSide || side > MaxPageSide {
			return fmt.Errorf("%w: page size %s is outside the supported %.0f to %.0f points per side",
				ErrOutOfRange, d, float64(MinPageSide), float64(MaxPageSide))
		}
	}

	return nil
}

// String formats the dimensions in points, such as "595.28x841.89pt".
func (d PageDim) String() string {
	return formatPoints(d.Width) + "x" + formatPoints(d.Height) + "pt"
}

func formatPoints(length Length) string {
	return strconv.FormatFloat(float64(length), 'f', 2, 64)
}

// namedPageSizes lists the supported paper names in portrait orientation.
func namedPageSizes() []namedPageSize {
	return []namedPageSize{
		{"A3", "297", "420", "mm"},
		{"A4", "210", "297", "mm"},
		{"A5", "148", "210", "mm"},
		{"B4", "250", "353", "mm"},
		{"B5", "176", "250", "mm"},
		{"Letter", "8.5", "11", "in"},
		{"Legal", "8.5", "14", "in"},
		{"Tabloid", "11", "17", "in"},
	}
}

// PageSizeNames lists the paper names ParsePageSize accepts, in the order the documentation uses.
func PageSizeNames() []string {
	names := make([]string, 0, len(namedPageSizes()))
	for _, named := range namedPageSizes() {
		names = append(names, named.name)
	}

	return names
}

// ParsePageSize parses "inherit", a paper name (A3, A4, A5, B4, B5, Letter, Legal, Tabloid; portrait,
// exact case), or WIDTHxHEIGHT with an optional unit shared by both sides, such as 210x297mm or 8.5x11in.
func ParsePageSize(text string) (PageSize, error) {
	if text == inheritSizeName {
		return PageSize{Inherit: true}, nil
	}

	for _, named := range namedPageSizes() {
		if named.name != text {
			continue
		}

		unit, _ := unitByName(named.unit)

		return explicitSize(named.width, named.height, unit, text)
	}

	width, rest, found := scanNumber(text)
	if !found || !strings.HasPrefix(rest, "x") {
		return PageSize{}, errInvalidPageSize(text)
	}

	height, suffix, found := scanNumber(rest[1:])

	unit, knownUnit := unitByName(suffix)
	if !found || !knownUnit {
		return PageSize{}, errInvalidPageSize(text)
	}

	return explicitSize(width, height, unit, text)
}

func explicitSize(width, height string, unit lengthUnit, text string) (PageSize, error) {
	widthLength, err := convert(width, unit)
	if err != nil {
		return PageSize{}, fmt.Errorf(valueCauseFormat, ErrInvalidPageSize, text, err)
	}

	heightLength, err := convert(height, unit)
	if err != nil {
		return PageSize{}, fmt.Errorf(valueCauseFormat, ErrInvalidPageSize, text, err)
	}

	dim, err := NewPageDim(widthLength, heightLength)
	if err != nil {
		return PageSize{}, fmt.Errorf(valueCauseFormat, ErrInvalidPageSize, text, err)
	}

	return PageSize{Dim: dim}, nil
}

// Validate checks an explicit size; an inherited size has nothing to check until it is resolved.
func (s PageSize) Validate() error {
	if s.Inherit {
		return nil
	}

	return s.Dim.Validate()
}

// String formats the size: "inherit", or the dimensions in points.
func (s PageSize) String() string {
	if s.Inherit {
		return inheritSizeName
	}

	return s.Dim.String()
}

func errInvalidPageSize(text string) error {
	return fmt.Errorf("%w %q: want inherit, a paper name (%s), or WIDTHxHEIGHT[unit] such as 210x297mm",
		ErrInvalidPageSize, text, strings.Join(PageSizeNames(), ", "))
}
