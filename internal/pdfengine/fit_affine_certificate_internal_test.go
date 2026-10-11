// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"math"
	"math/big"
	"testing"
)

func TestFitRowCertificateBoundsWitnessedRoundingLoss(t *testing.T) {
	t.Parallel()
	const origin = 0x1p53
	for _, row := range []struct {
		name   string
		matrix Affine
		box    [4]float64
		point  [2]float64
	}{
		{"first span has one zero endpoint", Affine{1, 0, 0, 1, -1, 0}, [4]float64{0, 1, origin + 2, 2}, [2]float64{origin + 2, 1}},
		{"second span has one zero endpoint", Affine{-1, 0, 1, 1, 0, 0}, [4]float64{1, 0, 1, origin + 2}, [2]float64{1, origin + 2}},
		{"span crosses zero", Affine{1, 0, 1, 1, 0, 0}, [4]float64{-origin, origin, origin, origin}, [2]float64{1, origin}},
		{"same sign spans", Affine{1, 0, 1, 1, 0, 0}, [4]float64{origin, origin / 2, origin, origin}, [2]float64{origin, origin/2 + 1}},
		{"only one magnitude ratio fits", Affine{1, 0, 1, 1, 0, 0}, [4]float64{origin, -4 * origin, 2 * origin, -origin}, [2]float64{origin + 2, -4*origin + 4}},
	} {
		t.Run(row.name, func(t *testing.T) {
			evaluation, finite := fitEvaluateRow(row.matrix, 0, row.box)
			if !finite {
				t.Fatal("finite witness was refused")
			}
			actual, exact := affineRowWitness(row.matrix, 0, row.point)
			loss := new(big.Rat).Sub(new(big.Rat).SetFloat64(actual), exact)
			loss.Abs(loss)
			if loss.Sign() <= 0 {
				t.Fatal("witness no longer demonstrates rounding loss")
			}
			bound := new(big.Rat).SetFloat64(evaluation.errorBound)
			if bound.Cmp(loss) < 0 {
				t.Fatalf("bound %s misses actual loss %s at %v", bound, loss, row.point)
			}
		})
	}
}

// affineRowWitness evaluates the declared unfused operation graph and, independently,
// the exact rational affine expression over the represented coefficients and point.
func affineRowWitness(matrix Affine, axis int, point [2]float64) (float64, *big.Rat) {
	first := float64(matrix[axis] * point[0])
	second := float64(matrix[axis+2] * point[1])
	actual := float64(float64(first+second) + matrix[axis+4])
	exact := new(big.Rat).SetFloat64(matrix[axis+4])
	for coordinate := range 2 {
		product := new(big.Rat).Mul(new(big.Rat).SetFloat64(matrix[axis+2*coordinate]), new(big.Rat).SetFloat64(point[coordinate]))
		exact.Add(exact, product)
	}
	return actual, exact
}

func TestFitRowCertificateRetainsTranslatedCornerRange(t *testing.T) {
	t.Parallel()
	evaluation, finite := fitEvaluateRow(Affine{2, 0, 3, 1, 7, 0}, 0, [4]float64{1, 2, 4, 5})
	if !finite {
		t.Fatal("finite translated rectangle refused")
	}
	// The four corners independently give 15, 21, 24 and 30 on the x axis.
	if evaluation.intervals[3] != (fitInterval{15, 30}) {
		t.Fatalf("translated corner range=%+v want [15,30]", evaluation.intervals[3])
	}
}

func TestFitChildCertificateAdmitsSignedSterbenzRatioBoundary(t *testing.T) {
	t.Parallel()
	const origin = 0x1p53
	source := Affine{-1, 0, 1, 1, 0, -origin}
	emitted := Affine{-2, 0, 2, 2, 0, -2 * origin}
	box := [4]float64{origin, origin, 2 * origin, 2 * origin}
	for _, point := range [][2]float64{{origin, origin}, {2 * origin, origin}, {origin, 2 * origin}, {origin + 2, origin + 4}} {
		assertFitExactPoint(t, source, point)
		assertFitExactPoint(t, emitted, point)
	}
	if err := fitChildGeometry(childProofFit(Affine{2, 0, 0, 2, 0, 0}, 2), source, emitted, box, box); err != nil {
		t.Fatalf("exact signed ratio-two cancellation refused: %v", err)
	}
}

func TestFitChildCertificatePreservesSignedInheritedRounding(t *testing.T) {
	t.Parallel()
	const origin = 0x1p53
	box := [4]float64{origin, 1, origin + 4, 2}
	point := [2]float64{origin + 2, 1}
	for _, sign := range []float64{-1, 1} {
		source := Affine{sign * 1.5, 0, 0, 1, -sign * 1.5 * origin, 0}
		emitted := Affine{sign * 3, 0, 0, 2, -sign * 3 * origin, 0}
		original, exact := affineRowWitness(source, 0, point)
		fitted, _ := affineRowWitness(emitted, 0, point)
		if exact.Cmp(new(big.Rat).SetFloat64(original)) == 0 || float64(2*original) != fitted {
			t.Fatal("signed source rounding witness lost inherited rounding or homogeneous scaling")
		}
		if err := fitChildGeometry(childProofFit(Affine{2, 0, 0, 2, 0, 0}, 2), source, emitted, box, box); err != nil {
			t.Fatalf("sign=%g exact homogeneous scaling refused: %v", sign, err)
		}
	}
}

func TestFitHomogeneousCertificateChecksLaterSubnormalOperation(t *testing.T) {
	t.Parallel()
	const normal = 0x1p-1022
	for _, row := range []struct {
		name               string
		box                [4]float64
		point, coefficient float64
	}{
		{"rounded normal endpoint", [4]float64{0, math.Nextafter(2*normal, 0), 1, math.Nextafter(2*normal, 0)}, math.Nextafter(2*normal, 0), .5},
		{"interior subnormal point", [4]float64{0, -1, 1, 1}, math.SmallestNonzeroFloat64, 1.5},
	} {
		t.Run(row.name, func(t *testing.T) {
			sourceMatrix := Affine{1, 0, 0, row.coefficient, 0, 0}
			emittedMatrix := Affine{2, 0, 0, 2 * row.coefficient, 0, 0}
			source, sourceFinite := fitEvaluateRow(sourceMatrix, 1, row.box)
			emitted, emittedFinite := fitEvaluateRow(emittedMatrix, 1, row.box)
			original, _ := affineRowWitness(sourceMatrix, 1, [2]float64{0, row.point})
			fitted, _ := affineRowWitness(emittedMatrix, 1, [2]float64{0, row.point})
			if !sourceFinite || !emittedFinite || float64(2*original) == fitted {
				t.Fatal("finite subnormal witness no longer demonstrates changed rounding")
			}
			if fitHomogeneousEvaluation(source, emitted) {
				t.Fatal("exact first operation concealed later subnormal mapping loss")
			}
		})
	}
}

func TestFitChildCertificateIgnoresUnmappedAxisRounding(t *testing.T) {
	t.Parallel()
	canvas := PageSize{math.SmallestNonzeroFloat64, 1000}
	fit, err := CanvasFit(canvas, canvas)
	if err != nil || fit.Original != canvas || fit.Target != canvas || fit.Scale != 1 || fit.Matrix != (Affine{1, 0, 0, 1, 0, 0}) {
		t.Fatalf("actual identity canvas fit unavailable: %+v, %v", fit, err)
	}
	// This exercises the arithmetic contract, not a complete tiny-page PDF.
	source := Affine{1, 0, 0, 1.5, 0, 0}
	box := [4]float64{0, 1, math.SmallestNonzeroFloat64, 2}
	moved := box
	moved[3] = math.Nextafter(2, math.Inf(1))
	for _, x := range []float64{0, math.SmallestNonzeroFloat64} {
		original, originalExact := affineRowWitness(source, 0, [2]float64{x, box[3]})
		fitted, fittedExact := affineRowWitness(source, 0, [2]float64{x, moved[3]})
		if original != fitted || originalExact.Cmp(fittedExact) != 0 || originalExact.Cmp(new(big.Rat).SetFloat64(original)) != 0 {
			t.Fatal("unchanged x projection gained rounding or movement")
		}
	}
	y, exactY := affineRowWitness(source, 1, [2]float64{0, moved[3]})
	loss := new(big.Rat).Sub(new(big.Rat).SetFloat64(y), exactY)
	loss.Abs(loss)
	if loss.Sign() <= 0 {
		t.Fatal("other axis no longer demonstrates rounding")
	}
	beforeY, _ := affineRowWitness(source, 1, [2]float64{0, box[3]})
	movement := new(big.Rat).Sub(new(big.Rat).SetFloat64(y), new(big.Rat).SetFloat64(beforeY))
	movement.Abs(movement)
	if new(big.Rat).Add(loss, movement).Cmp(big.NewRat(1, 1_000_000)) >= 0 {
		t.Fatal("other axis exceeded its independent one-micro-point budget")
	}
	if err := fitChildGeometry(fit, source, source, box, moved); err != nil {
		t.Fatalf("zero x loss was charged for unrelated y rounding: %v", err)
	}
}

func TestFitCertificateZeroEndpointsAndNormalULPBoundary(t *testing.T) {
	t.Parallel()
	for _, box := range [][4]float64{{0, 1, 1, 2}, {-1, 1, 0, 2}} {
		row, finite := fitEvaluateRow(Affine{1, 0, 0, 1, 0, 0}, 0, box)
		if !finite || row.intervals[0].normal() {
			t.Fatal("zero endpoint has zero minimum magnitude and cannot be a normal-only interval")
		}
	}
	const normal, least = 0x1p-1022, 0x1p-1074
	for _, magnitude := range []float64{math.Nextafter(normal, 0), normal, math.Nextafter(normal, math.Inf(1))} {
		if got := fitULP(magnitude); got != least {
			t.Fatalf("ULP at normal boundary %g: got %g want %g", magnitude, got, float64(least))
		}
	}
}

func TestFitProgramBoxAcceptsExactZeroEndpointScaling(t *testing.T) {
	t.Parallel()
	budget := math.Nextafter(math.Nextafter(0x1p-21, math.Inf(1)), math.Inf(1))
	extent := budget / fitRelativeTolerance
	fit, err := CanvasFit(PageSize{extent / 2, 1000}, PageSize{extent, 2000})
	if err != nil || fit.Matrix != (Affine{2, 0, 0, 2, 0, 0}) {
		t.Fatalf("ordinary twofold canvas fit unavailable: %+v, %v", fit, err)
	}

	const translation = 0x1p29
	unit := math.SmallestNonzeroFloat64
	for _, test := range []struct {
		name   string
		source Affine
		box    [4]float64
	}{
		{"first low touches zero", Affine{1, 0, 1, 1, translation, 0}, [4]float64{0, -unit, unit, -unit}},
		{"first high touches zero", Affine{-1, 0, 1, 1, translation, 0}, [4]float64{0, unit, unit, unit}},
		{"second low touches zero", Affine{1, 0, 1, 1, translation, 0}, [4]float64{-unit, 0, -unit, unit}},
		{"second high touches zero", Affine{1, 0, -1, 1, translation, 0}, [4]float64{unit, 0, unit, unit}},
	} {
		t.Run(test.name, func(t *testing.T) {
			emitted := test.source
			for index := range emitted {
				emitted[index] = float64(2 * emitted[index])
			}
			for _, x := range []float64{test.box[0], test.box[2]} {
				for _, y := range []float64{test.box[1], test.box[3]} {
					point := [2]float64{x, y}
					sourceActual, sourceExact := affineRowWitness(test.source, 0, point)
					emittedActual, emittedExact := affineRowWitness(emitted, 0, point)
					if new(big.Rat).Mul(sourceExact, big.NewRat(2, 1)).Cmp(emittedExact) != 0 ||
						new(big.Rat).Mul(new(big.Rat).SetFloat64(sourceActual), big.NewRat(2, 1)).Cmp(new(big.Rat).SetFloat64(emittedActual)) != 0 {
						t.Fatalf("rounded or exact mapping differs at %v", point)
					}
				}
			}

			inspector := newFitProgramInspector(guardContext(t))
			inspector.fit = fit
			state := fitGraphicsState{sourceMatrix: test.source, matrix: emitted}
			if err := inspector.programBox(test.box, test.box, &state); err != nil {
				t.Fatalf("exact zero-endpoint scaling refused: %v", err)
			}
		})
	}
}
