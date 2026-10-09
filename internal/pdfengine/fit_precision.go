// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import "math"

func exactFitScale(coefficient, low, high float64) bool {
	mantissa, exponent := math.Frexp(math.Abs(coefficient))
	if mantissa != 0.5 || !finiteAppearanceNumber(coefficient*low) || !finiteAppearanceNumber(coefficient*high) {
		return false
	}

	if exponent >= 1 {
		return true
	}
	// Downscaling an interval bounded away from zero is exact when every
	// result is normal. Otherwise retain the conservative general bound.
	const smallestNormal = 0x1p-1022

	minimumSource := min(math.Abs(low), math.Abs(high))
	minimumNormalSource := math.Ldexp(smallestNormal, 1-exponent)

	return !(low <= 0 && high >= 0) && minimumSource >= minimumNormalSource
}
