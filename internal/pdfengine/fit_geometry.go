// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	// Affine maps source default user space to output physical points using PDF's six coefficients.
	Affine [6]float64

	// PageFit captures original physical geometry and the final sheet transform once per geometry.
	PageFit struct {
		Media    [4]float64
		Crop     [4]float64
		Visible  [4]float64
		Original PageSize
		Target   PageSize
		Matrix   Affine
		Scale    float64
		UserUnit float64
		Rotation int
		HasCrop  bool
	}
)

const (
	fitThreeQuarterTurn  = 270
	fitSheetTolerance    = 0.000001
	fitRelativeTolerance = 0.000000001
)

var errFitGeometry = errors.New("page fitting geometry cannot be represented")

// CanvasFit captures the fit of a generated authored canvas with ordinary origin, units and rotation.
// Layout and report projections use this same authority as source-page fitting.
func CanvasFit(canvas, target PageSize) (PageFit, error) {
	box := rectangle{0, 0, canvas.Width, canvas.Height}
	return fitGeometry(pageGeometry{media: box, crop: box, visible: box, size: canvas, units: 1}, target)
}

// fitGeometry folds the displayed quarter-turn and physical units into one centered min-fit matrix.
func fitGeometry(geometry pageGeometry, target PageSize) (PageFit, error) {
	if !positiveFinite(target.Width) || !positiveFinite(target.Height) ||
		!positiveFinite(geometry.size.Width) || !positiveFinite(geometry.size.Height) {
		return PageFit{}, fmt.Errorf("%w: nonpositive or nonfinite dimensions", errFitGeometry)
	}

	scale := math.Min(target.Width/geometry.size.Width, target.Height/geometry.size.Height)

	s := scale * geometry.units
	if !positiveFinite(scale) || !positiveFinite(s) {
		return PageFit{}, fmt.Errorf("%w: scale overflows or underflows", errFitGeometry)
	}

	x := (target.Width - geometry.size.Width*scale) / 2
	y := (target.Height - geometry.size.Height*scale) / 2
	box := geometry.visible
	matrix := rotatedFitMatrix(geometry.rotation, s, x, y, box)

	fitted := PageFit{
		Media: rectangleArray(geometry.media), Crop: rectangleArray(geometry.crop), HasCrop: geometry.hasCrop,
		Visible:  [4]float64{box.x0, box.y0, box.x1, box.y1},
		Original: geometry.size, Target: target, Matrix: matrix, Scale: scale, UserUnit: geometry.units, Rotation: geometry.rotation,
	}
	if err := fitted.checkSerialization(x, y, geometry.size.Width*scale, geometry.size.Height*scale); err != nil {
		return PageFit{}, err
	}

	return fitted, nil
}

func rectangleArray(box rectangle) [4]float64 {
	return [4]float64{box.x0, box.y0, box.x1, box.y1}
}

func rotatedFitMatrix(rotation int, s, x, y float64, box rectangle) Affine {
	switch rotation {
	case quarterTurn:
		return Affine{0, -s, s, 0, x - s*box.y0, y + s*box.x1}
	case 2 * quarterTurn:
		return Affine{-s, 0, 0, -s, x + s*box.x1, y + s*box.y1}
	case fitThreeQuarterTurn:
		return Affine{0, s, -s, 0, x + s*box.y1, y - s*box.x0}
	default:
		return Affine{s, 0, 0, s, x - s*box.x0, y - s*box.y0}
	}
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

// Point applies the captured transform; all projected coordinates use this authority.
func (m Affine) Point(x, y float64) (float64, float64) {
	firstX, secondX := float64(m[0]*x), float64(m[2]*y)
	firstY, secondY := float64(m[1]*x), float64(m[3]*y)

	return float64(float64(firstX+secondX) + m[4]), float64(float64(firstY+secondY) + m[5])
}

// Bounds returns the axis-aligned bounds of the four transformed rectangle corners.
func (m Affine) Bounds(box [4]float64) [4]float64 {
	x0, y0 := m.Point(box[0], box[1])
	x1, y1 := m.Point(box[2], box[3])
	x2, y2 := m.Point(box[0], box[3])
	x3, y3 := m.Point(box[2], box[1])

	return [4]float64{min(x0, x1, x2, x3), min(y0, y1, y2, y3), max(x0, x1, x2, x3), max(y0, y1, y2, y3)}
}

// ClippedBounds projects the original visible intersection of a captured ink bounding rectangle.
// It projects existing measurements rather than remeasuring glyphs or opening a PDF.
func (f PageFit) ClippedBounds(box [4]float64) ([4]float64, bool) {
	clipped := [4]float64{max(box[0], f.Visible[0]), max(box[1], f.Visible[1]), min(box[2], f.Visible[2]), min(box[3], f.Visible[3])}
	if clipped[0] >= clipped[2] || clipped[1] >= clipped[3] {
		return [4]float64{}, false
	}

	return f.Matrix.Bounds(clipped), true
}

// pdfDecimal retains round-trip precision and uses PDF decimal syntax, never scientific notation.
func pdfDecimal(value float64) string {
	return types.Float(value).PDFString()
}

func (m Affine) contentMatrix() string {
	values := make([]string, len(m))
	for index, value := range m {
		values[index] = pdfDecimal(value)
	}

	return strings.Join(values, " ") + " cm"
}

// checkSerialization checks actual dictionary decimals and content coefficients, including cancellation
// at each visible edge. Sheet dimensions allow 1 micro-point representation error; source transforms
// must additionally retain their relative extent to one part in a billion.
func (f PageFit) checkSerialization(offsetX, offsetY, width, height float64) error {
	for _, value := range f.Matrix {
		if math.IsInf(value, 0) || math.IsNaN(value) {
			return fmt.Errorf("%w: nonfinite matrix", errFitGeometry)
		}
	}

	return f.checkArithmeticPrecision(offsetX, offsetY, width, height)
}

// Corner cancellation can accidentally yield correct edges while losing interior landmarks.
// Bound the complete rounded operation graph with the shared affine evaluator, rather than checking only endpoints.
func (f PageFit) checkArithmeticPrecision(offsetX, offsetY, width, height float64) error {
	for axis := range 2 {
		offset := offsetX
		if axis == 1 {
			offset = offsetY
		}

		evaluation, finite := fitEvaluateRow(f.Matrix, axis, f.Visible)
		if !finite {
			return fmt.Errorf("%w: nonfinite visible-domain evaluation on axis %d", errFitGeometry, axis)
		}

		extent := width
		if axis == 1 {
			extent = height
		}

		construction := fitPageConstructionDefect(f.Matrix, axis, f.Visible, offset, extent)

		budget := min(fitSheetTolerance, extent*fitRelativeTolerance)
		if fitUpperAdd(evaluation.errorBound, construction) > budget {
			return fmt.Errorf("%w: cannot certify interior landmark precision on axis %d", errFitGeometry, axis)
		}

		// The rounded far-edge reference is a separate, stricter predicate than the exact construction target.
		if math.Abs(evaluation.intervals[3].high-(offset+extent)) > budget {
			return fmt.Errorf("%w: serialized transform loses visible edge %d", errFitGeometry, axis+2)
		}
	}

	return nil
}

func validatedFitBox(box [4]float64) ([4]float64, error) {
	for _, value := range box {
		if !finiteAppearanceNumber(value) {
			return box, fmt.Errorf("%w: nonfinite rectangle coordinate", errFitGeometry)
		}
	}

	if box[0] >= box[2] || box[1] >= box[3] {
		return box, fmt.Errorf("%w: empty rectangle", errFitGeometry)
	}

	return box, nil
}
