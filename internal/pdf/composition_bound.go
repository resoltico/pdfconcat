// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"math"
	"math/big"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
)

type compositionState struct {
	source, fitted                   matrix.Matrix
	known, accurate                  bool
	fillPattern, strokePattern, font types.Name
}

func (p compositionState) concat(m matrix.Matrix) compositionState {
	p.source = m.Multiply(p.source)
	p.fitted = m.Multiply(p.fitted)
	p.known = false
	p.accurate = false
	return p
}

func finiteMatrix(m matrix.Matrix) bool {
	for _, row := range m {
		for _, v := range row {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
		}
	}
	return true
}
func rational(v float64) *big.Rat   { return new(big.Rat).SetFloat64(v) }
func ratAdd(a, b *big.Rat) *big.Rat { return new(big.Rat).Add(a, b) }
func ratSub(a, b *big.Rat) *big.Rat { return new(big.Rat).Sub(a, b) }
func ratMul(a, b *big.Rat) *big.Rat { return new(big.Rat).Mul(a, b) }
func ratAbsUpper(a *big.Rat) float64 {
	v, _ := new(big.Rat).Abs(a).Float64()
	if a.Sign() == 0 {
		return 0
	}
	return math.Nextafter(v, math.Inf(1))
}
func matrixEvaluationBound(m matrix.Matrix, maxX, maxY float64, axis int) float64 {
	const u = 0x1p-53
	gamma := math.Nextafter(4*u/(1-4*u), math.Inf(1))
	magnitude := errorAdd(errorAdd(errorMul(maxX, math.Abs(m[0][axis])), errorMul(maxY, math.Abs(m[1][axis]))), math.Abs(m[2][axis]))
	return errorAdd(errorMul(gamma, magnitude), 4*math.SmallestNonzeroFloat64)
}

// accurateComposition compares the actual concatenated CTM with intrinsic source
// execution followed by the outer map. Exact rational arithmetic is used only on
// the fixed-size current binary64 matrices, never an unbounded expression chain.
// Pulling the visible page frame back through the intrinsic CTM gives a domain
// for ALL visible local points. A singular/unbounded domain cannot be certified;
// no floating determinant/inverse representability proxy is imposed.
func accurateComposition(p compositionState, outer matrix.Matrix, frame types.Rectangle, target packet.Size) bool {
	if !finiteMatrix(p.source) || !finiteMatrix(p.fitted) || !finiteMatrix(outer) {
		return false
	}
	if p.source == matrix.IdentMatrix && p.fitted == outer {
		return true
	} // same operation as the outer certificate
	a, b, c, d := rational(p.source[0][0]), rational(p.source[0][1]), rational(p.source[1][0]), rational(p.source[1][1])
	e, f := rational(p.source[2][0]), rational(p.source[2][1])
	determinant := ratSub(ratMul(a, d), ratMul(b, c))
	if determinant.Sign() == 0 {
		return false
	}
	page := [4]types.Point{frame.LL, {X: frame.UR.X, Y: frame.LL.Y}, frame.UR, {X: frame.LL.X, Y: frame.UR.Y}}
	var local [4][2]*big.Rat
	maxX, maxY := 0.0, 0.0
	for i, corner := range page {
		x, y := ratSub(rational(corner.X), e), ratSub(rational(corner.Y), f)
		local[i][0] = new(big.Rat).Quo(ratSub(ratMul(x, d), ratMul(y, c)), determinant)
		local[i][1] = new(big.Rat).Quo(ratSub(ratMul(y, a), ratMul(x, b)), determinant)
		maxX = math.Max(maxX, ratAbsUpper(local[i][0]))
		maxY = math.Max(maxY, ratAbsUpper(local[i][1]))
	}
	if math.IsInf(maxX, 0) || math.IsInf(maxY, 0) {
		return false
	}
	var discrepancy [2]float64
	for axis := range 2 {
		// Exact represented source*outer versus actual concatenated fitted matrix.
		var delta [3]*big.Rat
		for row := range 3 {
			expected := new(big.Rat)
			for k := range 3 {
				expected = ratAdd(expected, ratMul(rational(p.source[row][k]), rational(outer[k][axis])))
			}
			delta[row] = ratSub(rational(p.fitted[row][axis]), expected)
		}
		for _, corner := range local {
			value := ratAdd(ratAdd(ratMul(corner[0], delta[0]), ratMul(corner[1], delta[1])), delta[2])
			discrepancy[axis] = math.Max(discrepancy[axis], ratAbsUpper(value))
		}
	}
	sourceError := [2]float64{matrixEvaluationBound(p.source, maxX, maxY, 0), matrixEvaluationBound(p.source, maxX, maxY, 1)}
	pageX := errorAdd(math.Max(math.Abs(frame.LL.X), math.Abs(frame.UR.X)), sourceError[0])
	pageY := errorAdd(math.Max(math.Abs(frame.LL.Y), math.Abs(frame.UR.Y)), sourceError[1])
	for axis, side := range [2]float64{target.Width, target.Height} {
		propagated := errorAdd(errorMul(sourceError[0], math.Abs(outer[0][axis])), errorMul(sourceError[1], math.Abs(outer[1][axis])))
		bound := errorAdd(discrepancy[axis], errorAdd(matrixEvaluationBound(p.fitted, maxX, maxY, axis), errorAdd(propagated, matrixEvaluationBound(outer, pageX, pageY, axis))))
		budget := forwardAccuracyULPs * (math.Nextafter(side, math.Inf(1)) - side)
		if math.IsNaN(bound) || math.IsInf(bound, 0) || bound > budget {
			return false
		}
	}
	return true
}
