// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type (
	// Length is a distance in PDF user-space units (points, 1/72 inch).
	Length float64

	// lengthUnit is a unit suffix and its size in points as the exact ratio numerator/denominator, so that
	// a length at a documented limit (5080mm is 14400pt) converts without rounding drift.
	lengthUnit struct {
		suffix                 string
		numerator, denominator float64
	}
)

const (
	// MaxLength bounds the magnitude of every length: the PDF implementation limit for page dimensions
	// (200 inches).
	MaxLength Length = 14400

	pointsPerInch    = 72
	mmPerInchTenth   = 254 // 25.4 millimetres per inch, scaled by ten.
	tenth            = 10
	valueCauseFormat = "%w %q: %w"
)

// lengthUnits lists the supported unit suffixes.
func lengthUnits() []lengthUnit {
	return []lengthUnit{
		{"pt", 1, 1},
		{"mm", pointsPerInch * tenth, mmPerInchTenth},
		{"cm", pointsPerInch * tenth * tenth, mmPerInchTenth},
		{"in", pointsPerInch, 1},
	}
}

// unitByName returns the unit with the given suffix; the empty suffix means points.
func unitByName(suffix string) (lengthUnit, bool) {
	if suffix == "" {
		return lengthUnit{"pt", 1, 1}, true
	}

	for _, unit := range lengthUnits() {
		if unit.suffix == suffix {
			return unit, true
		}
	}

	return lengthUnit{}, false
}

// NewLength checks that points is finite and within ±MaxLength.
func NewLength(points float64) (Length, error) {
	length := Length(points)

	err := length.Validate()
	if err != nil {
		return 0, err
	}

	return length, nil
}

// Validate reports whether the length is finite and within ±MaxLength.
func (l Length) Validate() error {
	value := float64(l)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%w: length %v is not a finite number", ErrOutOfRange, value)
	}

	if l < -MaxLength || l > MaxLength {
		return fmt.Errorf("%w: length %v is outside the supported range of ±%.0f points", ErrOutOfRange, value, float64(MaxLength))
	}

	return nil
}

// scanNumber splits a leading unsigned decimal number off text. The grammar is digits with an optional
// fraction, or a bare fraction: no sign, exponent, spaces, or "inf".
func scanNumber(text string) (string, string, bool) {
	integerDigits := countDigits(text)
	end := integerDigits
	fractionDigits := 0

	if end < len(text) && text[end] == '.' {
		fractionDigits = countDigits(text[end+1:])
		if fractionDigits == 0 {
			return "", text, false
		}

		end += 1 + fractionDigits
	}

	if integerDigits == 0 && fractionDigits == 0 {
		return "", text, false
	}

	return text[:end], text[end:], true
}

func countDigits(text string) int {
	count := 0
	for count < len(text) && text[count] >= '0' && text[count] <= '9' {
		count++
	}

	return count
}

// convert turns a scanned number in unit into a checked length.
func convert(number string, unit lengthUnit) (Length, error) {
	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: number %q is too large", ErrOutOfRange, number)
	}

	return NewLength(value * unit.numerator / unit.denominator)
}

// ParseLength parses a decimal number with an optional unit suffix (pt, mm, cm, in) and no surrounding
// space. A bare number is in points. The result is finite and within ±MaxLength.
func ParseLength(text string) (Length, error) {
	unsigned, negative := strings.CutPrefix(text, "-")
	number, suffix, found := scanNumber(unsigned)
	unit, knownUnit := unitByName(suffix)

	if !found || !knownUnit {
		return 0, fmt.Errorf("%w %q: want a number with optional unit pt, mm, cm, or in", ErrInvalidLength, text)
	}

	length, err := convert(number, unit)
	if err != nil {
		return 0, fmt.Errorf(valueCauseFormat, ErrInvalidLength, text, err)
	}

	if negative {
		length = -length
	}

	return length, nil
}
