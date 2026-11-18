// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

func fitToleranceRangeError() error {
	return fmt.Errorf("fitting tolerance must be from 0 through %g points", MaxSide)
}

// ParseFitToleranceNumber accepts the lexeme of a syntactically valid JSON number.
func ParseFitToleranceNumber(text string) (float64, error) {
	if strings.HasPrefix(text, "-") {
		significand := text
		if exponent := strings.IndexAny(text, "eE"); exponent >= 0 {
			significand = text[:exponent]
		}
		if strings.ContainsAny(significand, "123456789") {
			return 0, fitToleranceRangeError()
		}
	}
	points, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fitToleranceRangeError()
	}
	return NormalizeFitTolerance(points)
}

// ParseFitToleranceLength accepts the ordinary length grammar and rounds the
// exact physical conversion once, so equivalent unit spellings match alike.
func ParseFitToleranceLength(text string) (float64, error) {
	match := decimalLength.FindStringSubmatch(text)
	if match == nil {
		return 0, fmt.Errorf("invalid length %q; use a decimal with pt, mm, cm or in", text)
	}
	value, unit := strings.TrimSuffix(text, match[3]), match[3]
	if strings.HasPrefix(value, "-") || unit == "" || unit == "pt" {
		return ParseFitToleranceNumber(value)
	}
	numerator, denominator := int64(72), int64(1)
	switch unit {
	case "mm":
		numerator, denominator = 360, 127
	case "cm":
		numerator, denominator = 3600, 127
	}
	whole, fraction, _ := strings.Cut(value, ".")
	whole = strings.TrimLeft(whole, "0")
	if len(whole) > 5 {
		return 0, fitToleranceRangeError()
	}

	// A midpoint between binary64 values, multiplied by the integer unit
	// denominator, has at most 1075 fractional decimal digits. Carry through
	// any longer suffix of the product and retain a sticky digit. This preserves
	// which side of every midpoint the product lies on with bounded big integers.
	const midpointDigits = 1075
	carry, sticky := int64(0), false
	for i := len(fraction) - 1; i >= midpointDigits; i-- {
		product := int64(fraction[i]-'0')*numerator + carry
		carry = product / 10
		sticky = sticky || product%10 != 0
	}
	fraction = fraction[:min(len(fraction), midpointDigits)]
	digits := len(fraction)
	var product big.Int
	product.SetString("0"+whole+fraction, 10)
	product.Mul(&product, big.NewInt(numerator))
	product.Add(&product, big.NewInt(carry))
	if sticky {
		product.Mul(&product, big.NewInt(10))
		product.Add(&product, big.NewInt(1))
		digits++
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	scale.Mul(scale, big.NewInt(denominator))
	points, _ := new(big.Rat).SetFrac(&product, scale).Float64()
	return NormalizeFitTolerance(points)
}
