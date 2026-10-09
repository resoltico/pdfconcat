// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import "math"

type (
	fitInterval      struct{ low, high float64 }
	fitRowEvaluation struct {
		intervals  [4]fitInterval
		exact      [4]bool
		errorBound float64
	}
)

const (
	fitSmallestNormal  = 0x1p-1022
	fitSignificandBits = 53
)

// fitEvaluateRow bounds the declared unfused binary64 operation graph over
// every represented point in the box. Rounded operations are monotone.
func fitEvaluateRow(matrix Affine, axis int, box [4]float64) (fitRowEvaluation, bool) {
	var result fitRowEvaluation

	for coordinate := range 2 {
		coefficient := matrix[axis+2*coordinate]
		low, high := box[coordinate], box[coordinate+2]
		first, last := float64(coefficient*low), float64(coefficient*high)
		result.intervals[coordinate] = fitInterval{min(first, last), max(first, last)}
		result.exact[coordinate] = coefficient == 0 || exactFitScale(coefficient, low, high)
	}

	first, second := result.intervals[0], result.intervals[1]
	result.intervals[2] = fitInterval{float64(first.low + second.low), float64(first.high + second.high)}
	result.exact[2] = fitExactAddition(first, second)
	translation := fitInterval{matrix[4+axis], matrix[4+axis]}
	sum := result.intervals[2]
	result.intervals[3] = fitInterval{float64(sum.low + translation.low), float64(sum.high + translation.high)}

	result.exact[3] = fitExactAddition(sum, translation)
	for index, interval := range result.intervals {
		if !finiteAppearanceNumber(interval.low) || !finiteAppearanceNumber(interval.high) {
			return result, false
		}

		if !result.exact[index] {
			result.errorBound = fitUpperAdd(result.errorBound, fitULP(max(math.Abs(interval.low), math.Abs(interval.high))))
		}
	}

	return result, finiteAppearanceNumber(result.errorBound)
}

func fitExactAddition(first, second fitInterval) bool {
	if first.low == 0 && first.high == 0 || second.low == 0 && second.high == 0 {
		return true
	}

	if first.low <= 0 && first.high >= 0 || second.low <= 0 && second.high >= 0 || math.Signbit(first.low) == math.Signbit(second.low) {
		return false
	}

	firstMin, firstMax := min(math.Abs(first.low), math.Abs(first.high)), max(math.Abs(first.low), math.Abs(first.high))
	secondMin, secondMax := min(math.Abs(second.low), math.Abs(second.high)), max(math.Abs(second.low), math.Abs(second.high))

	return firstMin >= secondMax/2 && firstMax/2 <= secondMin
}

func fitULP(magnitude float64) float64 {
	if magnitude < fitSmallestNormal {
		return math.SmallestNonzeroFloat64
	}

	_, exponent := math.Frexp(magnitude)

	return math.Ldexp(1, exponent-fitSignificandBits)
}

func fitUpperAdd(first, second float64) float64 {
	if first == 0 {
		return second
	}

	if second == 0 {
		return first
	}

	return math.Nextafter(first+second, math.Inf(1))
}

func fitUpperMultiply(first, second float64) float64 {
	if first == 0 || second == 0 {
		return 0
	}

	return math.Nextafter(first*second, math.Inf(1))
}

func (interval fitInterval) normal() bool {
	return !(interval.low <= 0 && interval.high >= 0) && min(math.Abs(interval.low), math.Abs(interval.high)) > fitSmallestNormal
}

// Exact operations commute algebraically. Nonexact operations commute with
// signed power-of-two scaling only when both rounded results stay normal.
func fitHomogeneousEvaluation(source, emitted fitRowEvaluation) bool {
	for index := range source.exact {
		if source.exact[index] && emitted.exact[index] {
			continue
		}

		if !source.intervals[index].normal() || !emitted.intervals[index].normal() {
			return false
		}
	}

	return true
}
