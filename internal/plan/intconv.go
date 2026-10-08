// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import (
	"errors"
	"strconv"
	"strings"
)

// decimal accumulates the significant digits of a JSON number without floating point or big numbers:
// memory is constant and time linear in the text length.
type decimal struct {
	stored        [maxInt64Digits + 1]byte
	storedCount   int
	significant   int64 // digits after the leading zeros
	trailingZeros int64 // trailing zeros among the significant digits
}

const (
	maxInt64Digits = 19
	// maxExponent saturates exponent parsing: any larger exponent classifies identically.
	maxExponent = int64(1) << 40
	decimalBase = 10
)

var (
	errNotIntegral = errors.New("not an integer")
	errIntRange    = errors.New("integer out of range")
)

func (d *decimal) add(char byte) {
	if d.significant == 0 && char == '0' {
		return
	}

	d.significant++

	if char == '0' {
		d.trailingZeros++
	} else {
		d.trailingZeros = 0
	}

	if d.storedCount < len(d.stored) {
		d.stored[d.storedCount] = char
		d.storedCount++
	}
}

// digits returns the significant digits without trailing zeros, truncated to what can still matter.
func (d *decimal) digits() []byte {
	return d.stored[:min(d.significant-d.trailingZeros, int64(len(d.stored)))]
}

// scanMantissa reads the digits of the integer and fraction parts, and returns the length of the fraction.
func scanMantissa(text string) (*decimal, int64) {
	var (
		parsed         decimal
		fractionLength int64
	)

	integer, fraction, _ := strings.Cut(text, ".")

	for index := range len(integer) {
		parsed.add(integer[index])
	}

	for index := range len(fraction) {
		parsed.add(fraction[index])
	}

	fractionLength = int64(len(fraction))

	return &parsed, fractionLength
}

// scanExponent reads the optional exponent digits after the 'e' or 'E', saturating at maxExponent.
func scanExponent(tail string) int64 {
	negative := strings.HasPrefix(tail, "-")
	tail = strings.TrimLeft(tail, "+-")

	var exponent int64

	for index := range len(tail) {
		if exponent < maxExponent {
			exponent = exponent*decimalBase + int64(tail[index]-'0')
		}
	}

	if negative {
		return -exponent
	}

	return exponent
}

// exactInt64 converts the text of a valid JSON number to int64 only if its mathematical value is an
// integer within int64: 1, 1.0, 1e0, and 10e-1 convert; 1.5, 1e-1, 1e400, and 2^63 do not.
func exactInt64(text string) (int64, error) {
	unsigned, negative := strings.CutPrefix(text, "-")

	mantissa, exponentText, _ := strings.Cut(strings.ToLower(unsigned), "e")
	parsed, fractionLength := scanMantissa(mantissa)

	if parsed.significant == 0 {
		return 0, nil // zero in any spelling, including 0e999999999999
	}

	scale := scanExponent(exponentText) - fractionLength + parsed.trailingZeros
	if scale < 0 {
		return 0, errNotIntegral
	}

	digits := parsed.digits()
	if int64(len(digits))+scale > maxInt64Digits {
		return 0, errIntRange // at least 10^19, more than 2^63
	}

	sign := ""
	if negative {
		sign = "-"
	}

	// At most maxInt64Digits digits in total: ParseInt does the exact range check, including -2^63.
	value, err := strconv.ParseInt(sign+string(digits)+strings.Repeat("0", int(scale)), decimalBase, 64)
	if err != nil {
		return 0, errIntRange
	}

	return value, nil
}
