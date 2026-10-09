// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func verifyFittedSheets(ctx context.Context, pdf *model.Context, target PageSize) error {
	root, err := pdf.PagesContext(ctx)
	if err != nil {
		return fmt.Errorf("verify fitted page tree: %w", err)
	}

	number := 0

	err = walkPages(ctx, pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		number++

		geometry, readErr := decomposePage(ctx, pdf, page, inherited)
		if readErr != nil {
			return fmt.Errorf("fitted output page %d: %w", number, readErr)
		}

		if geometry.rotation != 0 || geometry.units != 1 || !geometry.hasCrop {
			return fmt.Errorf("%w: fitted output page %d has incorrect Rotate, UserUnit or CropBox", errFitGeometry, number)
		}

		return verifyFitBoxes(geometry, target, number)
	})
	if err != nil {
		return fmt.Errorf("verify fitted sheets: %w", err)
	}

	return nil
}

func verifyFitBoxes(geometry pageGeometry, target PageSize, number int) error {
	expected := [4]float64{0, 0, target.Width, target.Height}

	for _, box := range [][4]float64{rectangleArray(geometry.media), rectangleArray(geometry.crop)} {
		for index, value := range box {
			if math.Abs(value-expected[index]) > fitSheetTolerance {
				return fmt.Errorf("%w: fitted output page %d has incorrect MediaBox/CropBox edge %d", errFitGeometry, number, index)
			}
		}
	}

	return nil
}
