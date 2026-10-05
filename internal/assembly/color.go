// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	shortColorDigits = 3
	longColorDigits  = 6
)

// Color is an sRGB color.
type Color struct {
	R, G, B uint8
}

// ParseColor parses #RGB or #RRGGBB.
func ParseColor(text string) (Color, error) {
	digits, found := strings.CutPrefix(text, "#")
	if !found {
		return Color{}, fmt.Errorf("invalid color %q: want #RGB or #RRGGBB", text)
	}

	switch len(digits) {
	case shortColorDigits:
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	case longColorDigits:
	default:
		return Color{}, fmt.Errorf("invalid color %q: want #RGB or #RRGGBB", text)
	}

	var channels [3]uint8
	for i := range channels {
		value, err := strconv.ParseUint(digits[2*i:2*i+2], 16, 8)
		if err != nil {
			return Color{}, fmt.Errorf("invalid color %q: want #RGB or #RRGGBB", text)
		}

		channels[i] = uint8(value)
	}

	return Color{R: channels[0], G: channels[1], B: channels[2]}, nil
}

// String formats the color as #RRGGBB.
func (c Color) String() string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}
