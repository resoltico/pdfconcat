// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type fittedHotspot struct {
	quad []float64
	rect [4]float64
}

const (
	fitBorderKey  = "Border"
	keyQuadPoints = "QuadPoints"
)

func (i *fitInspector) linkBorder(ctx context.Context, link types.Dict) error {
	style, err := i.pdf.DereferenceDictContext(ctx, link["BS"])
	if err != nil {
		return fmt.Errorf("link /BS: %w", err)
	}

	var width float64
	if style != nil {
		width, err = i.linkStyleWidth(ctx, style)
	} else {
		width, err = i.linkArrayWidth(ctx, link[fitBorderKey])
	}

	if err != nil {
		return err
	}

	if width != 0 {
		return fmt.Errorf("%w: Link /Border or /BS has a painted or invalid effective width", errFitUnsupported)
	}

	return nil
}

func (i *fitInspector) linkStyleWidth(ctx context.Context, style types.Dict) (float64, error) {
	value, err := i.pdf.DereferenceContext(ctx, style["W"])
	if err != nil {
		return 0, fmt.Errorf("link /BS /W: %w", err)
	}

	if value == nil {
		return 1, nil
	}

	width, err := i.pdf.DereferenceNumberContext(ctx, value)
	if err != nil {
		return 0, fmt.Errorf("link /BS /W: %w", err)
	}

	return width, nil
}

func (i *fitInspector) linkArrayWidth(ctx context.Context, object types.Object) (float64, error) {
	border, err := i.pdf.DereferenceArrayContext(ctx, object)
	if err != nil {
		return 0, fmt.Errorf("link /Border: %w", err)
	}

	if len(border) == 0 {
		return 1, nil
	}

	if len(border) != 3 && len(border) != 4 {
		return 0, fmt.Errorf("%w: Link /Border must have three or four entries", errFitUnsupported)
	}

	values, err := i.fitNumbers(ctx, border[:3])
	if err != nil {
		return 0, fmt.Errorf("link /Border: %w", err)
	}

	return values[2], nil
}

func (i *fitInspector) fitNumbers(ctx context.Context, array types.Array) ([]float64, error) {
	values := make([]float64, len(array))
	for index, object := range array {
		value, err := i.pdf.DereferenceNumberContext(ctx, object)
		if err != nil {
			return nil, fmt.Errorf("coordinate %d: %w", index, err)
		}

		if !finiteAppearanceNumber(value) {
			return nil, fmt.Errorf("%w: coordinate %d is not finite numeric data", errFitUnsupported, index)
		}

		values[index] = value
	}

	return values, nil
}

func withinFitBox(inner, outer [4]float64) bool {
	return inner[0] >= outer[0] && inner[1] >= outer[1] && inner[2] <= outer[2] && inner[3] <= outer[3]
}

func (i *fitInspector) linkCoordinates(ctx context.Context, link types.Dict, fit PageFit) (fittedHotspot, error) {
	rect, found, err := boxOf(ctx, i.pdf, link, nil, "Rect")
	if err != nil {
		return fittedHotspot{}, fmt.Errorf("link /Rect: %w", err)
	}

	if !found {
		return fittedHotspot{}, fmt.Errorf("%w: Link /Rect is missing", errFitUnsupported)
	}

	box := rectangleArray(rect)
	if !withinFitBox(box, fit.Visible) {
		return fittedHotspot{}, fmt.Errorf("%w: Link /Rect is partly or wholly outside the original visible rectangle", errFitUnsupported)
	}

	fitted, err := fitTransformedBox(fit.Matrix, box)
	if err != nil {
		return fittedHotspot{}, fmt.Errorf("link /Rect: %w", err)
	}

	points, err := i.pdf.DereferenceArrayContext(ctx, link[keyQuadPoints])
	if err != nil {
		return fittedHotspot{}, fmt.Errorf(fitQuadFailureFormat, err)
	}

	transformed, err := i.quadPoints(ctx, points, box, fit.Matrix)
	if err != nil {
		return fittedHotspot{}, err
	}

	return fittedHotspot{rect: fitted, quad: transformed}, nil
}

func fitNumberArray(values []float64) types.Array {
	array := make(types.Array, len(values))
	for index, value := range values {
		array[index] = types.Float(value)
	}

	return array
}

func fitTransformedBox(matrix Affine, box [4]float64) ([4]float64, error) {
	// The maintained decimal serializer roundtrips every finite binary64 value exactly.
	// Validate the computed rectangle; numeric transform bounds are established separately.
	return validatedFitBox(matrix.Bounds(box))
}

func (i *fitInspector) quadPoints(ctx context.Context, array types.Array, rect [4]float64, matrix Affine) ([]float64, error) {
	if len(array)%fitQuadEntries != 0 {
		return nil, fmt.Errorf("%w: Link /QuadPoints must contain groups of eight numbers", errFitUnsupported)
	}

	values, err := i.fitNumbers(ctx, array)
	if err != nil {
		return nil, fmt.Errorf(fitQuadFailureFormat, err)
	}

	for index := 0; index < len(values); index += 2 {
		x, y := values[index], values[index+1]
		if x < rect[0] || x > rect[2] || y < rect[1] || y > rect[3] {
			return nil, fmt.Errorf("%w: Link /QuadPoints lies outside /Rect", errFitUnsupported)
		}

		values[index], values[index+1] = matrix.Point(x, y)
	}

	for _, value := range values {
		if !finiteAppearanceNumber(value) {
			return nil, fmt.Errorf("%w: Link /QuadPoints has a nonfinite transformed coordinate", errFitUnsupported)
		}
	}

	for index := 0; index < len(values); index += fitQuadEntries {
		if !convexFitQuad(values[index : index+fitQuadEntries]) {
			return nil, fmt.Errorf("%w: Link /QuadPoints is degenerate, crossed or unrepresentable", errFitUnsupported)
		}
	}

	return values, nil
}

func convexFitQuad(points []float64) bool {
	// PDF perimeter order and office producers' top-left/top-right/bottom-left/bottom-right order
	// both describe convex quads. The transform preserves the original slots; it never reorders them.
	return convexFitOrder(points, [4]int{0, 2, 4, 6}) || convexFitOrder(points, [4]int{0, 2, 6, 4})
}

func convexFitOrder(points []float64, order [4]int) bool {
	sign := 0.0

	for index, current := range order {
		next, following := order[(index+1)%len(order)], order[(index+2)%len(order)]

		cross := (points[next]-points[current])*(points[following+1]-points[next+1]) -
			(points[next+1]-points[current+1])*(points[following]-points[next])
		if !finiteAppearanceNumber(cross) || cross == 0 || sign != 0 && math.Signbit(cross) != math.Signbit(sign) {
			return false
		}

		sign = cross
	}

	return true
}
