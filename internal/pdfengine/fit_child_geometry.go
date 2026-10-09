// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"fmt"
	"math"
	"math/big"
)

// fitChildGeometry bounds fitting-introduced loss, preserving arithmetic
// already present in the source. The exact coefficient calculation has fixed
// size (six binary64 coefficients), rather than an unbounded transform history.
func fitChildGeometry(fit PageFit, source, emitted Affine, originalBox, emittedBox [4]float64) error {
	original, err := fitChildSourceEvaluations(fit, source, emitted, originalBox, emittedBox)
	if err != nil {
		return err
	}

	for axis := range 2 {
		if precisionErr := fitChildAxisPrecision(fit, source, emitted, originalBox, emittedBox, original, axis); precisionErr != nil {
			return precisionErr
		}
	}

	return nil
}

func fitChildSourceEvaluations(fit PageFit, source, emitted Affine, originalBox, emittedBox [4]float64) ([2]fitRowEvaluation, error) {
	var original [2]fitRowEvaluation

	for _, matrix := range []Affine{fit.Matrix, source, emitted} {
		for _, value := range matrix {
			if !finiteAppearanceNumber(value) {
				return original, fmt.Errorf("%w: nonfinite child matrix", errFitGeometry)
			}
		}
	}

	if !fitOrderedBox(originalBox) || !fitOrderedBox(emittedBox) {
		return original, fmt.Errorf("%w: invalid child domain", errFitGeometry)
	}

	for axis := range 2 {
		if originalBox[axis] < originalBox[axis+2] && emittedBox[axis] == emittedBox[axis+2] {
			return original, fmt.Errorf("%w: emitted child domain collapses", errFitGeometry)
		}
	}

	for axis := range 2 {
		row, finite := fitEvaluateRow(source, axis, originalBox)
		if !finite {
			return original, fmt.Errorf("%w: nonfinite original child evaluation", errFitGeometry)
		}

		original[axis] = row
	}

	return original, nil
}

func fitChildAxisPrecision(
	fit PageFit,
	source, emitted Affine,
	originalBox, emittedBox [4]float64,
	original [2]fitRowEvaluation,
	axis int,
) error {
	domain := [4]float64{
		min(originalBox[0], emittedBox[0]),
		min(originalBox[1], emittedBox[1]),
		max(originalBox[2], emittedBox[2]),
		max(originalBox[3], emittedBox[3]),
	}

	row, finite := fitEvaluateRow(emitted, axis, domain)
	if !finite {
		return fmt.Errorf("%w: nonfinite fitted child evaluation", errFitGeometry)
	}

	defect := fitCoefficientDefect(fit.Matrix, source, emitted, axis, originalBox)

	bound := defect
	if originalBox != emittedBox || !fitChildPassthrough(fit.Matrix, source, emitted, axis, original, row, defect) {
		bound = fitUpperAdd(bound, row.errorBound)
		for coordinate := range 2 {
			bound = fitUpperAdd(bound, fitUpperMultiply(math.Abs(fit.Matrix[axis+2*coordinate]), original[coordinate].errorBound))
		}
	}

	bound = fitUpperAdd(bound, fitBoxMovement(emitted, axis, originalBox, emittedBox))

	extent := fit.Original.Width * fit.Scale
	if axis == 1 {
		extent = fit.Original.Height * fit.Scale
	}

	budget := min(fitSheetTolerance, extent*fitRelativeTolerance)
	if !finiteAppearanceNumber(bound) || bound > budget {
		return fmt.Errorf("%w: cannot certify child precision on axis %d (bound %g pt, budget %g pt)", errFitGeometry, axis, bound, budget)
	}

	return nil
}

func fitOrderedBox(box [4]float64) bool {
	for _, value := range box {
		if !finiteAppearanceNumber(value) {
			return false
		}
	}

	return box[0] <= box[2] && box[1] <= box[3]
}

func fitChildPassthrough(outer, source, emitted Affine, axis int, original [2]fitRowEvaluation, row fitRowEvaluation, defect float64) bool {
	if defect != 0 || outer[4+axis] != 0 {
		return false
	}

	coordinate, coefficient := 0, outer[axis]
	if coefficient == 0 {
		coordinate, coefficient = 1, outer[axis+2]
	}

	if outer[axis] != 0 && outer[axis+2] != 0 {
		return false
	}

	if coefficient == 1 && source[coordinate] == emitted[axis] && source[coordinate+2] == emitted[axis+2] &&
		source[4+coordinate] == emitted[4+axis] {
		return true
	}

	mantissa, _ := math.Frexp(math.Abs(coefficient))

	return mantissa == 0.5 && fitHomogeneousEvaluation(original[coordinate], row)
}

func fitRat(value float64) *big.Rat { return new(big.Rat).SetFloat64(value) }

func fitRatUpper(value *big.Rat) float64 {
	result, exact := value.Float64()
	if exact {
		return result
	}

	return math.Nextafter(result, math.Inf(1))
}

// The ideal centered row is linear in this one source coordinate. Exact
// endpoint defects therefore bound construction error throughout the interval;
// the rounded evaluator separately bounds arithmetic inside that interval.
func fitPageConstructionDefect(matrix Affine, axis int, box [4]float64, offset, extent float64) float64 {
	coordinate, coefficient := 0, matrix[axis]
	if coefficient == 0 {
		coordinate, coefficient = 1, matrix[axis+2]
	}

	if matrix[axis] != 0 && matrix[axis+2] != 0 {
		return math.Inf(1)
	}

	first, last := box[coordinate], box[coordinate+2]
	if coefficient < 0 {
		first, last = last, first
	}

	maximum := new(big.Rat)
	for index, point := range []float64{first, last} {
		actual := new(big.Rat).Add(new(big.Rat).Mul(fitRat(coefficient), fitRat(point)), fitRat(matrix[4+axis]))

		expected := fitRat(offset)
		if index == 1 {
			expected.Add(expected, fitRat(extent))
		}

		defect := new(big.Rat).Sub(actual, expected)
		defect.Abs(defect)

		if defect.Cmp(maximum) > 0 {
			maximum = defect
		}
	}

	return fitRatUpper(maximum)
}

func fitCoefficientDefect(outer, source, emitted Affine, axis int, box [4]float64) float64 {
	result := new(big.Rat)

	for coefficient := range 3 {
		index := axis + 2*coefficient

		expected := new(big.Rat)
		for coordinate := range 2 {
			expected.Add(expected, new(big.Rat).Mul(fitRat(outer[axis+2*coordinate]), fitRat(source[coordinate+2*coefficient])))
		}

		if coefficient == 2 {
			expected.Add(expected, fitRat(outer[4+axis]))
		}

		difference := new(big.Rat).Sub(fitRat(emitted[index]), expected)
		difference.Abs(difference)

		if coefficient < 2 {
			difference.Mul(difference, fitRat(max(math.Abs(box[coefficient]), math.Abs(box[coefficient+2]))))
		}

		result.Add(result, difference)
	}

	return fitRatUpper(result)
}

func fitBoxMovement(matrix Affine, axis int, original, emitted [4]float64) float64 {
	result := new(big.Rat)
	for coordinate := range 2 {
		first := new(big.Rat).Sub(fitRat(original[coordinate]), fitRat(emitted[coordinate]))
		last := new(big.Rat).Sub(fitRat(original[coordinate+2]), fitRat(emitted[coordinate+2]))

		first.Abs(first)
		last.Abs(last)

		if last.Cmp(first) > 0 {
			first = last
		}

		result.Add(result, new(big.Rat).Mul(first, fitRat(math.Abs(matrix[axis+2*coordinate]))))
	}

	return fitRatUpper(result)
}
