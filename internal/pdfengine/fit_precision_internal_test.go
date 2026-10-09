// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestFitExactLargeOriginIntervals(t *testing.T) {
	t.Parallel()

	for _, origin := range []float64{1e10, 0x1p45, -0x1p45} {
		for _, scale := range []float64{0.5, 1, 2} {
			for _, rotation := range []int{0, 90, 180, 270} {
				assertFitExactInterval(t, origin, scale, rotation)
			}
		}
	}
}

func assertFitExactInterval(t *testing.T, origin, scale float64, rotation int) {
	t.Helper()

	target := PageSize{612 * scale, 1008 * scale}

	geometry := pageGeometry{
		visible: rectangle{origin, origin, origin + 612, origin + 1008},
		size:    PageSize{612, 1008}, units: 1, rotation: rotation,
	}
	if rotation == 90 || rotation == 270 {
		geometry.size = PageSize{1008, 612}
		target = PageSize{1008 * scale, 612 * scale}
	}

	fit, err := fitGeometry(geometry, target)
	if err != nil {
		t.Fatalf("origin=%g scale=%g rotation=%d: %v", origin, scale, rotation, err)
	}

	for _, point := range [][2]float64{
		{origin, origin},
		{math.Nextafter(origin, math.Inf(1)), origin + 17.25},
		{origin + 203.125, origin + 777.5},
	} {
		assertFitExactPoint(t, fit.Matrix, point)
	}
}

func assertFitExactPoint(t *testing.T, matrix Affine, point [2]float64) {
	t.Helper()

	for axis := range 2 {
		// Explicit conversions prevent compiler FMA from hiding loss in the
		// independent unfused multiply/add evaluation model.
		first := float64(matrix[axis] * point[0])
		second := float64(matrix[axis+2] * point[1])
		actual := float64(float64(first+second) + matrix[4+axis])

		expected := new(big.Rat).SetFloat64(matrix[4+axis])
		for coordinate := range 2 {
			product := new(big.Rat).Mul(new(big.Rat).SetFloat64(matrix[axis+2*coordinate]), new(big.Rat).SetFloat64(point[coordinate]))
			expected.Add(expected, product)
		}

		if expected.Cmp(new(big.Rat).SetFloat64(actual)) != 0 {
			t.Fatalf("inexact axis=%d point=%v matrix=%v actual=%g exact=%s", axis, point, matrix, actual, expected)
		}
	}
}

func TestFitExactScaleNormalBoundaryUsesSourcePreimage(t *testing.T) {
	t.Parallel()

	const smallestNormal = 0x1p-1022

	for _, coefficient := range []float64{0.5, 0.25, 0x1p-1022, 0x1p-1074} {
		_, exponent := math.Frexp(coefficient)

		boundary := math.Ldexp(smallestNormal, 1-exponent)
		for _, sign := range []float64{-1, 1} {
			at := sign * boundary

			below := sign * math.Nextafter(boundary, 0)
			if !exactFitScale(sign*coefficient, at, at) || exactFitScale(sign*coefficient, below, below) {
				t.Fatalf("normal preimage boundary misclassified: coefficient=%g source=%g", sign*coefficient, at)
			}
		}
	}
}

func TestFitScaleRejectsOverflowAndInexactUnderflow(t *testing.T) {
	t.Parallel()

	if exactFitScale(2, math.MaxFloat64/2, math.MaxFloat64) || exactFitScale(0.5, math.SmallestNonzeroFloat64, 1) {
		t.Fatal("overflow or inexact underflow accepted")
	}
}

func TestFitRetainsRoundedFarEdgePrecisionPredicate(t *testing.T) {
	t.Parallel()

	const origin = 0x1p35

	box := rectangle{origin, origin, origin + 511, origin + 1008}
	geometry := pageGeometry{media: box, crop: box, visible: box, size: PageSize{511, 1008}, units: 1}

	_, err := fitGeometry(geometry, PageSize{530.999998978, 1008})
	if !errors.Is(err, errFitGeometry) || !strings.Contains(err.Error(), "visible edge 2") {
		t.Fatalf("rounded-reference far edge guard lost: %v", err)
	}

	tiny := PageSize{math.SmallestNonzeroFloat64, 1}

	fit, err := CanvasFit(tiny, tiny)
	if err != nil || fit.Matrix != (Affine{1, 0, 0, 1, 0, 0}) {
		t.Fatalf("exact subnormal identity refused: %+v %v", fit, err)
	}
}
