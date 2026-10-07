// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"fmt"
	"strconv"
	"strings"
)

type (
	// Color is an sRGB color.
	Color struct {
		R, G, B uint8
	}

	// Fill is the background of a generated page: a color, or no painting. The zero value paints nothing.
	Fill struct {
		Color   Color
		Painted bool
	}
)

const (
	shortColorDigits = 3
	longColorDigits  = 6
	noFillName       = "none"
)

// ParseColor parses #RGB or #RRGGBB (hexadecimal digits of either case).
func ParseColor(text string) (Color, error) {
	digits, found := strings.CutPrefix(text, "#")
	if !found {
		return Color{}, errInvalidColor(text)
	}

	switch len(digits) {
	case shortColorDigits:
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	case longColorDigits:
	default:
		return Color{}, errInvalidColor(text)
	}

	var channels [3]uint8

	for index := range channels {
		value, err := strconv.ParseUint(digits[2*index:2*index+2], 16, 8)
		if err != nil {
			return Color{}, errInvalidColor(text)
		}

		channels[index] = uint8(value)
	}

	return Color{R: channels[0], G: channels[1], B: channels[2]}, nil
}

func errInvalidColor(text string) error {
	return fmt.Errorf("%w %q: want #RGB or #RRGGBB", ErrInvalidColor, text)
}

// String formats the color as #RRGGBB.
func (c Color) String() string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// FillColor returns a Fill that paints color.
func FillColor(color Color) Fill {
	return Fill{Color: color, Painted: true}
}

// ParseFill parses a color, or "none" for a page that is not painted.
func ParseFill(text string) (Fill, error) {
	if text == noFillName {
		return Fill{}, nil
	}

	color, err := ParseColor(text)
	if err != nil {
		return Fill{}, fmt.Errorf("%w %q: want #RGB, #RRGGBB, or none", ErrInvalidColor, text)
	}

	return FillColor(color), nil
}

// String formats the fill the way ParseFill accepts it.
func (f Fill) String() string {
	if !f.Painted {
		return noFillName
	}

	return f.Color.String()
}
