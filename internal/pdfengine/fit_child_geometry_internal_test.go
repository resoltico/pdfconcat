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

func childProofFit(matrix Affine, scale float64) PageFit {
	return PageFit{Matrix: matrix, Scale: scale, Original: PageSize{100, 100}}
}

func TestFitChildVerticalBudgetUsesScaledExtent(t *testing.T) {
	t.Parallel()
	fit, err := CanvasFit(PageSize{100, 100}, PageSize{200, 200})
	if err != nil || fit.Scale != 2 || fit.Matrix != (Affine{2, 0, 0, 2, 0, 0}) {
		t.Fatalf("twofold canvas fit: %+v err=%v", fit, err)
	}
	identity := Affine{1, 0, 0, 1, 0, 0}
	original := [4]float64{0, 0, 1, 1}
	for _, scenario := range []struct {
		name       string
		shift      float64
		lower      float64
		upper      float64
		wantRefuse bool
	}{
		{name: "safe movement above divided-height budget", shift: 5e-8, lower: 5e-8, upper: 2e-7},
		{name: "excess movement below absolute cap", shift: 2e-7, lower: 2e-7, upper: 1e-6, wantRefuse: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			movement := new(big.Rat).Mul(new(big.Rat).SetFloat64(scenario.shift), big.NewRat(2, 1))
			if movement.Cmp(new(big.Rat).SetFloat64(scenario.lower)) <= 0 || movement.Cmp(new(big.Rat).SetFloat64(scenario.upper)) >= 0 {
				t.Fatalf("independent fitted movement %s is outside (%g, %g)", movement.RatString(), scenario.lower, scenario.upper)
			}
			emittedBox := [4]float64{0, scenario.shift, 1, 1}
			err := fitChildGeometry(fit, identity, fit.Matrix, original, emittedBox)
			if scenario.wantRefuse {
				if !errors.Is(err, errFitGeometry) {
					t.Fatalf("excess vertical child movement admitted: %v", err)
				}
			} else if err != nil {
				t.Fatalf("safe vertical child movement refused: %v", err)
			}
		})
	}
}

func TestFitChildAcceptsInheritedVerticalRoundingUnderExactDoubling(t *testing.T) {
	t.Parallel()
	fit, err := CanvasFit(PageSize{100, 100}, PageSize{200, 200})
	if err != nil || fit.Scale != 2 || fit.Matrix != (Affine{2, 0, 0, 2, 0, 0}) {
		t.Fatalf("twofold canvas fit: %+v err=%v", fit, err)
	}
	const origin = 0x1p53
	source := Affine{1, 0, 0, 1.5, 0, -1.5 * origin}
	emitted := Affine{2, 0, 0, 3, 0, -3 * origin}
	box := [4]float64{0, origin, 1, origin + 4}
	row, finite := fitEvaluateRow(source, 1, box)
	if !finite || row.errorBound <= 2e-7 {
		t.Fatalf("inherited vertical rounding precondition: finite=%t bound=%g", finite, row.errorBound)
	}
	for _, y := range []float64{origin, origin + 2, origin + 4} {
		point := [2]float64{0, y}
		originalActual, originalExact := affineRowWitness(source, 1, point)
		emittedActual, emittedExact := affineRowWitness(emitted, 1, point)
		if new(big.Rat).Mul(originalExact, big.NewRat(2, 1)).Cmp(emittedExact) != 0 || float64(2*originalActual) != emittedActual {
			t.Fatalf("vertical power-of-two witness differs at %g: source=%g emitted=%g", y, originalActual, emittedActual)
		}
	}
	if err := fitChildGeometry(fit, source, emitted, box, box); err != nil {
		t.Fatalf("inherited vertical rounding was charged as fitting loss: %v", err)
	}
}

func TestFitChildRefusesRoundedInteriorDespiteMatchingZeroTerms(t *testing.T) {
	t.Parallel()
	fit, err := CanvasFit(PageSize{100, 100}, PageSize{150, 150})
	if err != nil || fit.Scale != 1.5 || fit.Matrix != (Affine{1.5, 0, 0, 1.5, 0, 0}) {
		t.Fatalf("one-and-a-half-fold canvas fit: %+v err=%v", fit, err)
	}
	const origin = 0x1p53
	point := [2]float64{origin + 2, 0}
	actual, exact := affineRowWitness(fit.Matrix, 0, point)
	difference := new(big.Rat).Sub(exact, new(big.Rat).SetFloat64(actual))
	difference.Abs(difference)
	if difference.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatalf("independent interior rounding witness is %s pt, want one point", difference.RatString())
	}
	identity := Affine{1, 0, 0, 1, 0, 0}
	box := [4]float64{origin, 0, origin + 4, 1}
	if err := fitChildGeometry(fit, identity, fit.Matrix, box, box); !errors.Is(err, errFitGeometry) {
		t.Fatalf("one-point fit-created interior loss bypassed child precision guard: %v", err)
	}
}

func TestFitChildIdentityPreservesInheritedZeroCrossingRounding(t *testing.T) {
	t.Parallel()
	fit, err := CanvasFit(PageSize{100, 100}, PageSize{100, 100})
	identity := Affine{1, 0, 0, 1, 0, 0}
	if err != nil || fit.Scale != 1 || fit.Matrix != identity {
		t.Fatalf("identity canvas fit: %+v err=%v", fit, err)
	}
	const low, high = 0x1p53, 0x1p70
	source := Affine{1.5, 0, -0.15, 1, 0, 0}
	box := [4]float64{low, low, high, high}
	row, finite := fitEvaluateRow(source, 0, box)
	if !finite || row.errorBound <= 1e-7 || row.intervals[2].normal() || row.exact[2] {
		t.Fatalf("inherited zero-crossing precondition: finite=%t row=%+v", finite, row)
	}
	for _, point := range [][2]float64{{low, low}, {high, high}, {high, low}} {
		actual, exact := affineRowWitness(source, 0, point)
		emittedActual, emittedExact := affineRowWitness(source, 0, point)
		if actual != emittedActual || exact.Cmp(emittedExact) != 0 {
			t.Fatalf("identity fit changed inherited arithmetic at %v", point)
		}
		if point == ([2]float64{high, high}) {
			inherited := new(big.Rat).Sub(exact, new(big.Rat).SetFloat64(actual))
			inherited.Abs(inherited)
			if inherited.Cmp(big.NewRat(1, 1)) < 0 {
				t.Fatalf("independent inherited rounding witness is %s pt, want at least one point", inherited.RatString())
			}
		}
	}
	if err := fitChildGeometry(fit, source, source, box, box); err != nil {
		t.Fatalf("identity fit charged inherited zero-crossing rounding: %v", err)
	}
}

func TestFitChildChangedZeroWidthDomainCannotBypassPrecision(t *testing.T) {
	t.Parallel()
	fit, err := CanvasFit(PageSize{100, 100}, PageSize{100, 100})
	identity := Affine{1, 0, 0, 1, 0, 0}
	if err != nil || fit.Scale != 1 || fit.Matrix != identity {
		t.Fatalf("identity canvas fit: %+v err=%v", fit, err)
	}
	const moved = 1.1e-8
	source := Affine{1e18, 0, 0, 1, 0, 0}
	emitted := Affine{2e18, 0, 0, 1, 0, 0}
	originalBox, emittedBox := [4]float64{0, 0, 0, 1}, [4]float64{moved, 0, moved, 1}
	point := [2]float64{moved, 0}
	actual, exact := affineRowWitness(emitted, 0, point)
	loss := new(big.Rat).Sub(exact, new(big.Rat).SetFloat64(actual))
	loss.Abs(loss)
	if loss.Cmp(new(big.Rat).SetFloat64(1e-7)) <= 0 {
		t.Fatalf("independent changed-domain rounding witness is %s pt, want above budget", loss.RatString())
	}
	if err := fitChildGeometry(fit, source, emitted, originalBox, emittedBox); !errors.Is(err, errFitGeometry) {
		t.Fatalf("changed zero-width child domain bypassed fit precision guard: %v", err)
	}
}

func TestFitChildUnchangedZeroWidthCoordinateIgnoresInertCoefficient(t *testing.T) {
	t.Parallel()
	fit, err := CanvasFit(PageSize{100, 100}, PageSize{100, 100})
	identity := Affine{1, 0, 0, 1, 0, 0}
	if err != nil || fit.Scale != 1 || fit.Matrix != identity {
		t.Fatalf("identity canvas fit: %+v err=%v", fit, err)
	}
	source := Affine{1e18, 0, 0, 1, 0, 0}
	emitted := Affine{2e18, 0, 0, 1, 0, 0}
	box := [4]float64{0, 0, 0, 1}
	for _, point := range [][2]float64{{0, 0}, {0, 0.5}, {0, 1}} {
		originalActual, originalExact := affineRowWitness(source, 0, point)
		emittedActual, emittedExact := affineRowWitness(emitted, 0, point)
		if originalActual != emittedActual || originalExact.Cmp(emittedExact) != 0 {
			t.Fatalf("constant-zero child coordinate changed at %v", point)
		}
	}
	if err := fitChildGeometry(fit, source, emitted, box, box); err != nil {
		t.Fatalf("inert coefficient on unchanged zero-width domain refused: %v", err)
	}
}

func TestFitChildVerticalIdentityUsesTranslationComponent(t *testing.T) {
	t.Parallel()
	fit, err := CanvasFit(PageSize{100, 100}, PageSize{100, 100})
	identity := Affine{1, 0, 0, 1, 0, 0}
	if err != nil || fit.Scale != 1 || fit.Matrix != identity {
		t.Fatalf("identity canvas fit: %+v err=%v", fit, err)
	}
	const low, high = 0x1p53, 0x1p70
	source := Affine{1, 1.5, 0, -0.15, 0, 0}
	box := [4]float64{low, low, high, high}
	row, finite := fitEvaluateRow(source, 1, box)
	if !finite || row.errorBound <= 1e-7 || row.intervals[2].normal() || row.exact[2] {
		t.Fatalf("inherited vertical zero-crossing precondition: finite=%t row=%+v", finite, row)
	}
	point := [2]float64{high, high}
	actual, exact := affineRowWitness(source, 1, point)
	inherited := new(big.Rat).Sub(exact, new(big.Rat).SetFloat64(actual))
	inherited.Abs(inherited)
	if inherited.Cmp(big.NewRat(1, 1)) < 0 {
		t.Fatalf("independent vertical inherited rounding witness is %s pt, want at least one point", inherited.RatString())
	}
	if err := fitChildGeometry(fit, source, source, box, box); err != nil {
		t.Fatalf("vertical identity fit charged inherited rounding: %v", err)
	}
}

func TestFitPageConstructionDefectKeepsZeroAndTiedEndpointValues(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name                 string
		coefficient, shift   float64
		box                  [4]float64
		offset, extent, want float64
	}{
		{name: "positive zero tied", coefficient: 0, shift: 0.5, box: [4]float64{1, 0, 3, 1}, offset: 0, extent: 1, want: 0.5},
		{name: "negative zero tied", coefficient: math.Copysign(0, -1), shift: 0.5, box: [4]float64{1, 0, 3, 1}, offset: 0, extent: 1, want: 0.5},
		{name: "zero asymmetric", coefficient: 0, shift: 0, box: [4]float64{1, 0, 3, 1}, offset: 0, extent: 1, want: 1},
		{name: "nonzero tied", coefficient: 1, shift: 0, box: [4]float64{0, 0, 1, 1}, offset: 0.5, extent: 1, want: 0.5},
		{name: "negative reversed", coefficient: -1, shift: 0, box: [4]float64{0, 0, 1, 1}, offset: -1, extent: 1, want: 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			matrix := Affine{scenario.coefficient, 0, 0, 1, scenario.shift, 0}
			if got := fitPageConstructionDefect(matrix, 0, scenario.box, scenario.offset, scenario.extent); got != scenario.want {
				t.Fatalf("endpoint defect %g, want %g", got, scenario.want)
			}
		})
	}
}

func TestFitBoxMovementKeepsEqualAndUnequalEdgeMaxima(t *testing.T) {
	t.Parallel()
	matrix := Affine{2, 0, 3, 1, 0, 0}
	original := [4]float64{0, 0, 10, 20}
	for _, scenario := range []struct {
		name    string
		emitted [4]float64
		want    float64
	}{
		{name: "equal opposite edges", emitted: [4]float64{1, -2, 9, 22}, want: 8},
		{name: "last edge larger", emitted: [4]float64{1, -2, 8, 22}, want: 10},
		{name: "first edge larger", emitted: [4]float64{3, -2, 9, 22}, want: 12},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			if got := fitBoxMovement(matrix, 0, original, scenario.emitted); got != scenario.want {
				t.Fatalf("weighted edge movement %g, want %g", got, scenario.want)
			}
		})
	}
}

func TestFitChildRejectsCrossCornerOverflowAndInteriorLoss(t *testing.T) {
	t.Parallel()

	if _, finite := fitEvaluateRow(Affine{1e308, 0, -1e308, 1, 0, 0}, 0, [4]float64{-1, -1, 1, 1}); finite {
		t.Fatal("finite diagonals concealed overflowing cross-corners")
	}

	const origin = 0x1p53

	source := Affine{1, 0, 0, 1, -origin, 0}
	emitted := Affine{1.5, 0, 0, 1.5, -1.5 * origin, 0}
	box := [4]float64{origin, 0, origin + 4, 2}
	fit := childProofFit(Affine{1.5, 0, 0, 1.5, 0, 0}, 1.5)
	product := float64(1.5 * float64(origin+2))

	actual := float64(product - 1.5*origin)
	if actual != 4 {
		t.Fatalf("interior negative control lost its failure boundary: %g", actual)
	}

	if err := fitChildGeometry(fit, source, emitted, box, box); !errors.Is(err, errFitGeometry) {
		t.Fatalf("corner-correct interior loss admitted: %v", err)
	}
}

func TestFitChildPreservesInheritedArithmeticAndExactCancellation(t *testing.T) {
	t.Parallel()

	const origin = 0x1p53

	identity := Affine{1, 0, 0, 1, 0, 0}

	for _, source := range []Affine{
		{1.5, 0, 1, 1, -1.5 * origin, 0},
		{1, 0, -1, 1, 0, 0},
	} {
		box := [4]float64{origin, origin, origin + 4, origin + 4}
		if err := fitChildGeometry(childProofFit(identity, 1), source, source, box, box); err != nil {
			t.Fatalf("identity fit rejected unchanged source arithmetic: %v", err)
		}
	}

	row, finite := fitEvaluateRow(Affine{1, 0, -1, 1, 0, 0}, 0, [4]float64{origin, origin, origin + 4, origin + 4})
	if !finite || row.errorBound != 0 {
		t.Fatalf("two-term Sterbenz cancellation not proved: %+v", row)
	}

	for _, scale := range []float64{0.5, 2} {
		source := Affine{1.5, 0, 0, 1, -1.5 * origin, 0}
		emitted := Affine{1.5 * scale, 0, 0, scale, -1.5 * scale * origin, 0}

		box := [4]float64{origin, 1, origin + 4, 2}
		if err := fitChildGeometry(childProofFit(Affine{scale, 0, 0, scale, 0, 0}, scale), source, emitted, box, box); err != nil {
			t.Fatalf("homogeneous scale=%g rejected inherited rounding: %v", scale, err)
		}
	}
}

func TestFitChildRejectsTranslationReassociationAndChangedClip(t *testing.T) {
	t.Parallel()

	const origin = 0x1p53

	box := [4]float64{origin, 0, origin, 1}

	reassociationErr := fitChildGeometry(childProofFit(Affine{1, 0, 0, 1, -origin, 0}, 1),
		Affine{1, 0, 0, 1, 1, 0}, Affine{1, 0, 0, 1, 1 - origin, 0}, box, box)
	if !errors.Is(reassociationErr, errFitGeometry) {
		t.Fatalf("coefficient-exact reassociation admitted: %v", reassociationErr)
	}

	identity := Affine{1, 0, 0, 1, 0, 0}
	clipErr := fitChildGeometry(childProofFit(identity, 1), identity, identity,
		[4]float64{0, 0, 1, 1}, [4]float64{0.001, 0, 1, 1})

	if !errors.Is(clipErr, errFitGeometry) {
		t.Fatalf("serialized clipping edge movement admitted: %v", clipErr)
	}

	for _, bad := range []Affine{{math.NaN(), 0, 0, 1, 0, 0}, {math.Inf(1), 0, 0, 1, 0, 0}} {
		if err := fitChildGeometry(childProofFit(identity, 1), bad, identity, box, box); !errors.Is(err, errFitGeometry) {
			t.Fatal("nonfinite child matrix admitted")
		}
	}
}

func TestFitChildHomogeneousShortcutRejectsRoundedNormalBoundary(t *testing.T) {
	t.Parallel()

	const normal = 0x1p-1022

	point := math.Nextafter(2*normal, 0)
	box := [4]float64{point, 0, point, 1}
	source, finiteSource := fitEvaluateRow(Affine{0.5, 0, 0, 1, 0, 0}, 0, box)

	emitted, finiteEmitted := fitEvaluateRow(Affine{1, 0, 0, 2, 0, 0}, 0, box)
	if !finiteSource || !finiteEmitted || float64(2*float64(0.5*point)) == point {
		t.Fatal("normal boundary negative control did not demonstrate inherited rounding")
	}

	if fitHomogeneousEvaluation(source, emitted) {
		t.Fatal("rounded-up normal boundary received an unsound zero-loss proof")
	}
}

func TestFitPageConstructionPaysInteriorPrecisionBudget(t *testing.T) {
	t.Parallel()

	const unit = 0x1p-20

	anchor := float64(-6004799503160659 * unit)

	geometry := pageGeometry{visible: rectangle{anchor, 0, anchor + 1024, 1024}, size: PageSize{1024, 1024}, units: 1}
	if _, err := fitGeometry(geometry, PageSize{1536 + 9*unit, 1536}); !errors.Is(err, errFitGeometry) {
		t.Fatalf("rounded centering plus interior arithmetic exceeded budget without refusal: %v", err)
	}
}

func TestFitChildChangedBoxCannotBypassInheritedRoundingBound(t *testing.T) {
	t.Parallel()

	const (
		first = float64(0x1p53 - 1)
		last  = float64(0x1p53)
	)

	coefficient := float64(1e-6)
	translation := -float64(coefficient * first)
	matrix := Affine{coefficient, 0, 0, 1, translation, 0}
	original, emitted := [4]float64{first - 1, 0, first, 1}, [4]float64{first, 0, last, 1}

	fit := PageFit{Matrix: Affine{1, 0, 0, 1, 0, 0}, Scale: 1, Original: PageSize{2000, 2000}}
	if err := fitChildGeometry(fit, matrix, matrix, original, emitted); !errors.Is(err, errFitGeometry) {
		t.Fatalf("moved box bypassed rounded edge error: %v", err)
	}
}

func TestFitChildRefusesInvalidDomainsAndEvaluationOverflow(t *testing.T) {
	t.Parallel()

	identity := Affine{1, 0, 0, 1, 0, 0}

	box := [4]float64{0, 0, 1, 1}
	for _, tc := range []struct {
		name        string
		message     string
		source      Affine
		emitted     Affine
		originalBox [4]float64
		emittedBox  [4]float64
	}{
		{"nonfinite domain", "invalid child domain", identity, identity, [4]float64{0, 0, math.Inf(1), 1}, box},
		{"reversed domain", "invalid child domain", identity, identity, [4]float64{2, 0, 1, 1}, box},
		{"collapsed domain", "emitted child domain collapses", identity, identity, box, [4]float64{0, 0, 0, 1}},
		{
			"source arithmetic overflow", "nonfinite original child evaluation",
			Affine{math.MaxFloat64, 0, 0, 1, 0, 0},
			identity,
			[4]float64{2, 0, 3, 1},
			[4]float64{2, 0, 3, 1},
		},
		{
			"fitted arithmetic overflow", "nonfinite fitted child evaluation",
			identity,
			Affine{math.MaxFloat64, 0, 0, 1, 0, 0},
			[4]float64{2, 0, 3, 1},
			[4]float64{2, 0, 3, 1},
		},
		{"changed far edge", "cannot certify child precision", identity, identity, box, [4]float64{0, 0, 2, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := fitChildGeometry(childProofFit(identity, 1), tc.source, tc.emitted, tc.originalBox, tc.emittedBox)
			if !errors.Is(err, errFitGeometry) || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unsafe child domain/operation admitted: %v", err)
			}
		})
	}

	shear := Affine{1, 0, 1, 1, 0, 0}
	if err := fitChildGeometry(childProofFit(shear, 1), identity, shear, [4]float64{1, 1, 2, 2}, [4]float64{1, 1, 2, 2}); err != nil {
		t.Fatalf("representable composed child refused: %v", err)
	}

	if !math.IsInf(fitPageConstructionDefect(shear, 0, box, 0, 1), 1) {
		t.Fatal("general shear misidentified as an axis-only page fit")
	}
}
