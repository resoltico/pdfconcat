// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"math"
	"strconv"

	"github.com/resoltico/pdfconcat/internal/assembly"
)

const (
	fitQuarterTurn      = 90
	fitThreeQuarterTurn = 270
)

func (v *validator) fitting() *fault {
	if v.report.Fit == nil {
		if len(v.report.Geometries) > 0 {
			return newFault("/geometries", CodeInvalidValue, "fitting geometry needs its effective fit declaration")
		}
	} else if found := fitDeclarationFault(v.report.Fit); found != nil {
		return found
	}

	for index := range v.report.Geometries {
		if found := geometryFault("/geometries/"+strconv.Itoa(index), &v.report.Geometries[index]); found != nil {
			return found
		}
	}

	pages, pageFault := v.fitSourcePageCounts()
	if pageFault != nil {
		return pageFault
	}

	for index := range v.report.Sources {
		if found := v.sourceFits(index, pages[index]); found != nil {
			return found
		}
	}

	for index := range v.report.Styles {
		if found := v.styleFit(index); found != nil {
			return found
		}
	}

	return nil
}

func (v *validator) fitSourcePageCounts() ([]*int64, *fault) {
	pages := make([]*int64, len(v.report.Sources))
	if v.report.Fit == nil {
		return pages, nil
	}

	for index := range v.report.Parts {
		part := &v.report.Parts[index]
		if part.Source != nil && part.Pages != nil {
			if prior := pages[*part.Source]; prior != nil && *prior != *part.Pages {
				return nil, newFault(
					at(pointerParts, index, "/pages"),
					CodeInvalidValue,
					"source occurrences disagree on captured page count",
				)
			}

			pages[*part.Source] = part.Pages
		}
	}

	return pages, nil
}

func fitDeclarationFault(fit *FitDeclaration) *fault {
	target, err := assembly.ParseFitTarget(fit.Paper)
	if err != nil {
		return newFault("/fit/paper", CodeInvalidValue, "fit paper must be A4 or Legal")
	}

	dim, err := target.Dim()
	if err != nil || fit.Size.Origin != SizeFitTarget || fit.Size.Width != float64(dim.Width) || fit.Size.Height != float64(dim.Height) {
		return newFault("/fit/size", CodeInvalidValue, "fit size must resolve the named portrait target with fit_target origin")
	}

	if fit.Location != nil {
		return locationFault("/fit/location", fit.Location)
	}

	return nil
}

func geometryFault(pointer string, geometry *Geometry) *fault {
	for _, box := range [][4]float64{geometry.Media, geometry.Crop, geometry.Visible} {
		if !validGeometryBox(box) {
			return newFault(pointer, CodeInvalidValue, "original boxes must be finite positive rectangles")
		}
	}

	if !geometryWithin(geometry.Visible, geometry.Media) || !geometryWithin(geometry.Visible, geometry.Crop) ||
		!geometry.HasCrop && geometry.Crop != geometry.Media {
		return newFault(pointer+"/visible_box", CodeInvalidValue, "visible geometry must remain inside resolved original boxes")
	}

	for _, value := range []float64{geometry.Width, geometry.Height, geometry.Scale, geometry.UserUnit} {
		if !positiveGeometry(value) {
			return newFault(pointer, CodeInvalidValue, "original physical dimensions, units and fit scale must be positive and finite")
		}
	}

	switch geometry.Rotation {
	case 0, fitQuarterTurn, 2 * fitQuarterTurn, fitThreeQuarterTurn:
	default:
		return newFault(pointer+"/rotation", CodeInvalidValue, "effective rotation must be a normalized quarter-turn")
	}

	return fitMatrixFault(pointer+"/matrix", geometry.Matrix)
}

func fitMatrixFault(pointer string, matrix [6]float64) *fault {
	maximum := 0.0

	for index, value := range matrix {
		if !within(value, -math.MaxFloat64, math.MaxFloat64) {
			return newFault(pointer, CodeInvalidValue, "matrix coefficients must be finite")
		}

		if index < geometryBoxCoordinates {
			maximum = max(maximum, math.Abs(value))
		}
	}

	if maximum == 0 || (matrix[0]/maximum)*(matrix[3]/maximum)-
		(matrix[1]/maximum)*(matrix[2]/maximum) <= 0 {
		return newFault(pointer, CodeInvalidValue, "fit matrix must be invertible and orientation preserving")
	}

	return nil
}

func positiveGeometry(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validGeometryBox(box [4]float64) bool {
	for _, value := range box {
		if !within(value, -math.MaxFloat64, math.MaxFloat64) {
			return false
		}
	}

	return box[2] > box[0] && box[3] > box[1]
}

func geometryWithin(inner, outer [4]float64) bool {
	return inner[0] >= outer[0] && inner[1] >= outer[1] && inner[2] <= outer[2] && inner[3] <= outer[3]
}

func (v *validator) geometryReference(pointer string, index int) *fault {
	if v.report.Fit == nil || index < 0 || index >= len(v.report.Geometries) {
		return newFault(pointer, CodeDanglingReference, "geometry reference needs a captured fit table and declaration")
	}

	return nil
}

func (v *validator) sourceFits(index int, pages *int64) *fault {
	source := &v.report.Sources[index]
	pointer := at(pointerSources, index, "/geometries")

	next := 1
	for position, interval := range source.Geometries {
		if interval.First != next || interval.Last < next {
			return newFault(at(pointer, position, ""), CodeInvalidValue, "source geometry ranges must cover consecutive pages")
		}

		if found := v.geometryReference(at(pointer, position, "/geometry"), interval.Geometry); found != nil {
			return found
		}

		next = interval.Last + 1
	}

	if v.report.Fit != nil && pages != nil && int64(next) != *pages+1 {
		return newFault(pointer, CodeInvalidValue, "fit geometry ranges must cover every captured source page")
	}

	return nil
}

func (v *validator) styleFit(index int) *fault {
	style := &v.report.Styles[index]
	pointer := "/styles/" + strconv.Itoa(index)

	if style.Geometry == nil {
		if style.FinalText != nil || style.Size.Origin == SizeFitTarget {
			return newFault(pointer, CodeInvalidValue, "fit-origin and final placement require captured geometry")
		}

		if v.report.Fit != nil {
			return newFault(pointer+"/geometry", CodeDanglingReference, "a resolved fitted canvas needs its geometry reference")
		}

		return nil
	}

	if found := v.geometryReference(pointer+"/geometry", *style.Geometry); found != nil {
		return found
	}

	return finalTextFault(pointer+"/final_text", style)
}

func finalTextFault(pointer string, style *Style) *fault {
	placement := style.FinalText
	if placement == nil {
		return nil
	}

	if style.Text == nil || !positiveGeometry(placement.FontSize) ||
		placement.Bounds != nil && !validBounds(placement.Bounds) ||
		placement.InkBounds != nil && !validBounds(placement.InkBounds) {
		return newFault(pointer, CodeInvalidValue, "final text placement needs authored text and finite physical geometry")
	}

	return nil
}
