// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestFitPreservesDisplayedOrientationAndCenters(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		rotation    int
		first, last [2]float64
	}{
		{0, [2]float64{50, 0}, [2]float64{550, 1000}},
		{90, [2]float64{0, 650}, [2]float64{600, 350}},
		{180, [2]float64{550, 1000}, [2]float64{50, 0}},
		{270, [2]float64{600, 350}, [2]float64{0, 650}},
	} {
		g := pageGeometry{visible: rectangle{10, 20, 110, 220}, size: PageSize{100, 200}, units: 1, rotation: test.rotation}
		if test.rotation == 90 || test.rotation == 270 {
			g.size = PageSize{200, 100}
		}

		fit, err := fitGeometry(g, PageSize{600, 1000})
		if err != nil {
			t.Fatal(err)
		}

		firstX, firstY := fit.Matrix.Point(10, 20)
		lastX, lastY := fit.Matrix.Point(110, 220)

		if [2]float64{firstX, firstY} != test.first || [2]float64{lastX, lastY} != test.last {
			t.Fatalf("rotation %d: %v %v", test.rotation, [2]float64{firstX, firstY}, [2]float64{lastX, lastY})
		}
	}
}

func TestFitUserUnitChangesProvenanceWithoutDoubleScaling(t *testing.T) {
	t.Parallel()

	g := pageGeometry{visible: rectangle{0, 0, 100, 200}, size: PageSize{100, 200}, units: 1}

	first, err := fitGeometry(g, PageSize{612, 1008})
	if err != nil {
		t.Fatal(err)
	}

	g.units, g.size = 2, PageSize{200, 400}

	second, err := fitGeometry(g, PageSize{612, 1008})
	if err != nil {
		t.Fatal(err)
	}

	if first.Matrix != second.Matrix || first.Scale != 2*second.Scale || first.Original == second.Original {
		t.Fatalf("unit application/provenance: %+v %+v", first, second)
	}
}

func TestFitSerializedPrecisionAndFiniteBoundaries(t *testing.T) {
	t.Parallel()

	for _, side := range []float64{0.00000000000001, 0.0000001, 1, 1000000000} {
		fit, err := fitGeometry(pageGeometry{
			visible: rectangle{0, 0, side, side},
			size:    PageSize{side, side}, units: 1,
		}, PageSize{612, 1008})
		if err != nil {
			t.Fatalf("finite side %v: %v", side, err)
		}

		if strings.ContainsAny(fit.Matrix.contentMatrix(), "eE") {
			t.Fatalf("scientific PDF number: %s", fit.Matrix.contentMatrix())
		}

		bounds := fit.Matrix.Bounds(fit.Visible)
		if math.Abs(bounds[0]) > 0.000001 || math.Abs(bounds[1]-198) > 0.000001 ||
			math.Abs(bounds[2]-612) > 0.000001 || math.Abs(bounds[3]-810) > 0.000001 {
			t.Fatalf("serialized landmark bounds: %v", bounds)
		}
	}

	for _, g := range []pageGeometry{
		{visible: rectangle{1e14, 1e14, 1e14 + 1, 1e14 + 1}, size: PageSize{1, 1}, units: 1},
		{visible: rectangle{0, 0, 1, 1}, size: PageSize{math.SmallestNonzeroFloat64, 1}, units: 1},
		{visible: rectangle{0, 0, 1, 1}, size: PageSize{1, 1}, units: math.MaxFloat64},
		{size: PageSize{0, 1}, units: 1},
	} {
		if _, err := fitGeometry(g, PageSize{612, 1008}); !errors.Is(err, errFitGeometry) {
			t.Fatalf("unrepresentable geometry accepted: %+v: %v", g, err)
		}
	}
}

func TestFitSerializationRejectsBrokenClipAndMatrix(t *testing.T) {
	t.Parallel()

	for _, clip := range [][4]float64{{0, 0, 0, 1}, {0, 0, 1, math.Inf(1)}, {0, 0, math.NaN(), 1}} {
		if _, err := validatedFitBox(clip); !errors.Is(err, errFitGeometry) {
			t.Fatalf("invalid clip accepted: %v %v", clip, err)
		}
	}

	for _, matrix := range []Affine{{0, 0, 0, 0, 0, 0}, {math.Inf(1), 0, 0, 1, 0, 0}, {1, 0, 0, 1, 5, 0}} {
		fit := PageFit{Visible: [4]float64{0, 0, 1, 1}, Matrix: matrix}
		if err := fit.checkSerialization(0, 0, 1, 1); !errors.Is(err, errFitGeometry) {
			t.Fatalf("invalid transform accepted: %v %v", matrix, err)
		}
	}
}

func TestFitConstructorRejectsOverflowingEmissionDespiteFiniteSource(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		message string
		box     rectangle
		target  PageSize
	}{
		{
			"translation overflow", "nonfinite matrix",
			rectangle{1e308, 0, math.Nextafter(1e308, math.Inf(1)), 1},
			PageSize{math.MaxFloat64, math.MaxFloat64},
		},
		{
			"evaluation overflow", "nonfinite visible-domain evaluation",
			rectangle{1e308, 0, math.MaxFloat64, 1},
			PageSize{1e308, 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			geometry := pageGeometry{
				media:   tc.box,
				crop:    tc.box,
				visible: tc.box,
				size:    PageSize{tc.box.x1 - tc.box.x0, tc.box.y1 - tc.box.y0},
				units:   1,
			}
			_, err := fitGeometry(geometry, tc.target)

			if !errors.Is(err, errFitGeometry) || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unsafe emitted arithmetic admitted/misclassified: %v", err)
			}
		})
	}
}
