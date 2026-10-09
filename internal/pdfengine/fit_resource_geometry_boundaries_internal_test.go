// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitResourceMatrixAndBoxPreserveNativeCancellation(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := inspector.resourceMatrix(ctx, types.Dict{fitMatrix: types.NewIntegerArray(1, 0, 0, 1, 0, 0)}, fitMatrix)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resource matrix erased cancellation: %v", err)
	}

	_, err = inspector.resourceBoxValues(ctx, types.Dict{keyBBox: types.NewIntegerArray(0, 0, 1, 1)}, keyBBox)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resource bbox erased cancellation: %v", err)
	}
}

func TestFitResourceMatrixAndBBoxMalformedDeclarationsRefuse(t *testing.T) {
	t.Parallel()

	invalidMatrices := []types.Object{
		types.Integer(1), types.NewIntegerArray(1, 0, 0),
		types.Array{types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(1), types.Name(guardBadMetadata), types.Integer(0)},
		types.Array{types.Integer(1), types.Integer(0), types.Integer(0), types.Integer(1), types.Float(math.Inf(1)), types.Integer(0)},
	}
	for n, object := range invalidMatrices {
		t.Run("matrix/"+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			inspector := newFitProgramInspector(pdf)
			if _, err := inspector.resourceMatrix(t.Context(), types.Dict{fitMatrix: object}, fitMatrix); err == nil {
				t.Fatal("malformed original resource matrix accepted")
			}
		})
	}

	invalidBoxes := []types.Object{
		types.Integer(1), types.NewIntegerArray(0, 0, 1),
		types.Array{types.Integer(0), types.Integer(0), types.Name(guardBadMetadata), types.Integer(1)},
		types.Array{types.Integer(0), types.Integer(0), types.Float(math.Inf(1)), types.Integer(1)},
	}
	for n, object := range invalidBoxes {
		t.Run("box/"+strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			inspector := newFitProgramInspector(pdf)
			if _, err := inspector.resourceBoxValues(t.Context(), types.Dict{keyBBox: object}, keyBBox); err == nil {
				t.Fatal("malformed original resource bbox accepted")
			}
		})
	}
}

func TestFitOriginalContentArithmeticCannotHideOverflowBehindShrink(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	large := strconv.FormatFloat(1e308, 'f', -1, 64)
	page := types.Dict{keyContents: types.StreamDict{
		Dict: types.Dict{}, Content: []byte(large + " 0 0 " + large + " 0 0 cm 2 0 0 2 0 0 cm"),
	}}
	fit := PageFit{
		Matrix: Affine{.5, 0, 0, .5, 0, 0}, Visible: [4]float64{0, 0, 100, 100},
		Original: PageSize{100, 100}, Target: PageSize{50, 50}, Scale: .5, UserUnit: 1,
	}

	if err := newFitProgramInspector(pdf).inspectPage(t.Context(), page, nil, fit); !errors.Is(err, errFitGeometry) {
		t.Fatalf("shrinking did not refuse original arithmetic overflow: %v", err)
	}
}

func TestFitOriginalGlyphArithmeticCannotHideOverflowBehindShrink(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	glyph := guardStream(t, pdf, guardZeroGlyphMetrics, nil)
	font := guardType3(t, pdf, types.Dict{"A": glyph})
	scope := types.Dict{keyFont: types.Dict{"F": font}}
	large := strconv.FormatFloat(1e308, 'f', -1, 64)
	page := types.Dict{keyContents: types.StreamDict{
		Dict: types.Dict{}, Content: []byte(large + " 0 0 " + large + " 0 0 cm BT /F 1 Tf 2 0 0 2 0 0 Tm (A) Tj ET"),
	}}
	fit := PageFit{
		Matrix: Affine{.5, 0, 0, .5, 0, 0}, Visible: [4]float64{0, 0, 100, 100},
		Original: PageSize{100, 100}, Target: PageSize{50, 50}, Scale: .5, UserUnit: 1,
	}

	if err := newFitProgramInspector(pdf).inspectPage(t.Context(), page, scope, fit); !errors.Is(err, errFitGeometry) {
		t.Fatalf("shrinking did not refuse original glyph arithmetic overflow: %v", err)
	}
}
