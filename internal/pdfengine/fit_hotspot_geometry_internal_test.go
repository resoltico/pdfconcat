// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitLinkUsesEffectiveBorderStyleBeforeIgnoredBorder(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		border types.Object
		style  types.Object
		pass   bool
	}{
		{types.NewNumberArray(0, 0, 0), nil, true},
		{types.NewNumberArray(0, 0, 0), types.Dict{}, false},
		{types.NewNumberArray(0, 0, 1), types.Dict{"W": types.Integer(0)}, true},
		{types.Name("ignored malformed border"), types.Dict{"W": types.Integer(0)}, true},
		{nil, nil, false},
		{types.NewNumberArray(0, 0, 0), types.Dict{"W": types.Float(math.NaN())}, false},
		{types.NewNumberArray(0, 0, 0), types.Dict{"W": types.Name("not numeric")}, false},
	} {
		pdf, _, link := fitLinkFixture(t)
		link[fitBorderKey], link["BS"] = test.border, test.style

		_, err := inspectPageFits(t.Context(), pdf, PageSize{612, 1008})
		if test.pass != (err == nil) || err != nil && !errors.Is(err, errFitUnsupported) {
			t.Fatalf("effective border judgment pass=%t: %v", test.pass, err)
		}
	}
}

func TestFitQuadSerializationPreservesExactSourceVertexSlots(t *testing.T) {
	t.Parallel()
	pdf, _, _ := fitLinkFixture(t)

	inspector := fitInspector{pdf: pdf}
	for _, test := range []struct{ source, want []float64 }{
		{[]float64{10, 20, 30, 20, 30, 40, 10, 40}, []float64{60, 70, 60, 110, 20, 110, 20, 70}},
		{[]float64{10, 40, 30, 40, 10, 20, 30, 20}, []float64{20, 70, 20, 110, 60, 70, 60, 110}},
	} {
		actual, err := inspector.quadPoints(
			t.Context(),
			fitNumberArray(test.source),
			[4]float64{10, 20, 30, 40},
			Affine{0, 2, -2, 0, 100, 50},
		)
		if err != nil || !slices.Equal(actual, test.want) {
			t.Fatalf("vertex-slot oracle: %v, want %v: %v", actual, test.want, err)
		}

		actual[0], actual[2] = actual[2], actual[0]

		actual[1], actual[3] = actual[3], actual[1]
		if slices.Equal(actual, test.want) {
			t.Fatal("slot permutation escaped the exact-slot oracle")
		}
	}
}

func TestFitQuadRejectsDegenerateScrambledAndOutOfRectangleCoordinates(t *testing.T) {
	t.Parallel()
	pdf, _, _ := fitLinkFixture(t)

	inspector := fitInspector{pdf: pdf}
	for _, points := range [][]float64{
		{10, 20, 10, 20, 30, 40, 10, 40},
		{10, 20, 20, 20, 30, 20, 10, 40},
		{10, 20, 30, 40, 30, 20, 10, 40},
		{10, 20, 30, 20, 20, 25, 10, 40},
		{9, 20, 30, 20, 30, 40, 10, 40},
	} {
		_, err := inspector.quadPoints(t.Context(), fitNumberArray(points), [4]float64{10, 20, 30, 40}, Affine{1, 0, 0, 1, 0, 0})
		if !errors.Is(err, errFitUnsupported) {
			t.Fatalf("invalid activation quadrilateral passed: %v", err)
		}
	}
}

func TestFitHotspotPrecisionUsesEachAxisExtent(t *testing.T) {
	t.Parallel()

	box := [4]float64{0, 0.4000000000004, 0.000000000001, 600.4000000000004}

	actual, err := fitTransformedBox(Affine{1, 0, 0, 1, 0, 0}, box)
	if err != nil || actual[0] >= actual[2] || actual[1] >= actual[3] {
		t.Fatalf("narrow hotspot incorrectly couples vertical precision to horizontal extent: %v %v", actual, err)
	}

	tiny := [4]float64{0, 0, 0.0000000000001, 600}
	if actual, err = fitTransformedBox(Affine{1, 0, 0, 1, 0, 0}, tiny); err != nil || actual != tiny {
		t.Fatalf("roundtrip writer lost a valid narrow hotspot: %v %v", actual, err)
	}

	if _, err = fitTransformedBox(Affine{1, 0, 0, 1, 0, 0}, [4]float64{0, 0, 0, 600}); !errors.Is(err, errFitGeometry) {
		t.Fatalf("genuinely empty hotspot was accepted: %v", err)
	}
}

func TestFitQuadRejectsOverflowFromFiniteCoordinatesAndCoefficients(t *testing.T) {
	t.Parallel()
	pdf, _, _ := fitLinkFixture(t)

	inspector := fitInspector{pdf: pdf}
	_, err := inspector.quadPoints(
		t.Context(), types.NewNumberArray(1, 1, 2, 1, 2, 2, 1, 2),
		[4]float64{1, 1, 2, 2}, Affine{math.MaxFloat64, 0, 0, 1, 0, 0},
	)

	if !errors.Is(err, errFitUnsupported) {
		t.Fatalf("computed nonfinite activation coordinates accepted: %v", err)
	}
}
