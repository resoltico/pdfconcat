// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func childProofFit(matrix Affine, scale float64) PageFit {
	return PageFit{Matrix: matrix, Scale: scale, Original: PageSize{100, 100}}
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
