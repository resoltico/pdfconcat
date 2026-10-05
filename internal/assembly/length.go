// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Length is a distance in PDF user-space units (points, 1/72 inch).
type Length float64

// maxLength bounds any user-supplied length to the PDF implementation limit
// for page dimensions (200 inches).
const maxLength Length = 14400

const (
	pointsPerInch            = 72.0
	millimetresPerInch       = 25.4
	millimetresPerCentimetre = 10
	pointsPerMillimetre      = pointsPerInch / millimetresPerInch
	pointsPerCentimetre      = pointsPerMillimetre * millimetresPerCentimetre
)

// lengthUnit is a unit suffix and its size in points.
type lengthUnit struct {
	suffix string
	points float64
}

// lengthUnits lists the supported unit suffixes.
func lengthUnits() []lengthUnit {
	return []lengthUnit{
		{"pt", 1},
		{"mm", pointsPerMillimetre},
		{"cm", pointsPerCentimetre},
		{"in", pointsPerInch},
	}
}

// ParseLength parses a decimal number with an optional unit suffix
// (pt, mm, cm, in). A bare number is in points.
func ParseLength(text string) (Length, error) {
	original := text
	text = strings.TrimSpace(text)
	scale := 1.0

	for _, unit := range lengthUnits() {
		if number, found := strings.CutSuffix(text, unit.suffix); found {
			text, scale = number, unit.points

			break
		}
	}

	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("invalid length %q: want a number with optional unit pt, mm, cm, or in", original)
	}

	length := Length(number * scale)
	if length < -maxLength || length > maxLength {
		return 0, fmt.Errorf("length %q is outside the supported range of ±%.0f points", original, float64(maxLength))
	}

	return length, nil
}
