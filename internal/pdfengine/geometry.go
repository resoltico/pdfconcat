// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"errors"
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	// PageSize is a page's visible size in physical points (1/72 inch), as a viewer shows it.
	PageSize struct {
		Width, Height float64
	}

	// rectangle is a normalized box: x0 < x1 and y0 < y1.
	rectangle struct {
		x0, y0, x1, y1 float64
	}
)

const (
	quarterTurn  = 90
	fullTurn     = 360
	boxEntries   = 4
	defaultUnits = 1.0
	// maxRotateMagnitude keeps the conversion of /Rotate to int exact; real values are within a few turns.
	maxRotateMagnitude = 1 << 30
)

var (
	errMediaBoxMissing  = errors.New("MediaBox is missing")
	errCropOutsideMedia = errors.New("CropBox does not overlap MediaBox")
	errSizeOverflow     = errors.New("page size overflows")
	errBoxEntryCount    = errors.New("box has the wrong number of numbers")
	errBoxNonFinite     = errors.New("has a non-finite number")
	errBoxEmpty         = errors.New("is empty")
	errRotationNotRight = errors.New("rotation is not a multiple of 90")
	errUserUnitInvalid  = errors.New("UserUnit is not a positive finite number")
)

// visibleSize computes the visible size of a leaf page.
//
// The visible rectangle is the effective CropBox constrained to the effective MediaBox, or the MediaBox
// when there is no CropBox. Both boxes are inherited through the page tree independently of one another.
// A rotation of 90 or 270 degrees swaps width and height, and the page's UserUnit (default 1) scales
// both to physical points. Missing, non-numeric, non-finite, empty or non-intersecting boxes, a rotation
// that is not a multiple of 90, and a non-positive UserUnit are errors.
func visibleSize(pdf *model.Context, page types.Dict, inherited inheritedAttrs) (PageSize, error) {
	media, found, err := boxOf(pdf, page, inherited.mediaBox, keyMediaBox)
	if err != nil {
		return PageSize{}, err
	}

	if !found {
		return PageSize{}, errMediaBoxMissing
	}

	visible := media

	crop, found, err := boxOf(pdf, page, inherited.cropBox, keyCropBox)
	if err != nil {
		return PageSize{}, err
	}

	if found {
		visible = rectangle{
			x0: math.Max(media.x0, crop.x0), y0: math.Max(media.y0, crop.y0),
			x1: math.Min(media.x1, crop.x1), y1: math.Min(media.y1, crop.y1),
		}
		if visible.x0 >= visible.x1 || visible.y0 >= visible.y1 {
			return PageSize{}, errCropOutsideMedia
		}
	}

	rotation, err := rotationOf(pdf, page, inherited.rotate)
	if err != nil {
		return PageSize{}, err
	}

	units, err := userUnitOf(pdf, page)
	if err != nil {
		return PageSize{}, err
	}

	size := PageSize{Width: (visible.x1 - visible.x0) * units, Height: (visible.y1 - visible.y0) * units}
	if rotation%(2*quarterTurn) != 0 {
		size.Width, size.Height = size.Height, size.Width
	}

	if math.IsInf(size.Width, 0) || math.IsInf(size.Height, 0) {
		return PageSize{}, errSizeOverflow
	}

	return size, nil
}

// boxOf reads the rectangle entry key of page, or else the inherited one; found is false for an absent or
// null entry.
func boxOf(pdf *model.Context, page types.Dict, inherited types.Object, key string) (rectangle, bool, error) {
	object, own := page.Find(key)
	if !own {
		object = inherited
	}

	if object == nil {
		return rectangle{}, false, nil
	}

	array, err := pdf.DereferenceArray(object)
	if err != nil {
		return rectangle{}, false, fmt.Errorf("%s: %w", key, err)
	}

	if array == nil {
		return rectangle{}, false, nil
	}

	box, err := boxFromArray(pdf, array, key)
	if err != nil {
		return rectangle{}, false, err
	}

	return box, true, nil
}

// boxFromArray converts the four numbers of array, named by key in errors, to a normalized non-empty rectangle.
func boxFromArray(pdf *model.Context, array types.Array, key string) (rectangle, error) {
	if len(array) != boxEntries {
		return rectangle{}, fmt.Errorf("%w: %s has %d numbers, want %d", errBoxEntryCount, key, len(array), boxEntries)
	}

	var corner [boxEntries]float64

	for index, entry := range array {
		value, err := pdf.DereferenceNumber(entry)
		if err != nil {
			return rectangle{}, fmt.Errorf("%s: %w", key, err)
		}

		if math.IsNaN(value) || math.IsInf(value, 0) {
			return rectangle{}, fmt.Errorf("%s %w", key, errBoxNonFinite)
		}

		corner[index] = value
	}

	box := rectangle{
		x0: math.Min(corner[0], corner[2]), y0: math.Min(corner[1], corner[3]),
		x1: math.Max(corner[0], corner[2]), y1: math.Max(corner[1], corner[3]),
	}
	if box.x0 == box.x1 || box.y0 == box.y1 {
		return rectangle{}, fmt.Errorf("%s %w", key, errBoxEmpty)
	}

	return box, nil
}

// rotationOf reads /Rotate of page, or else the inherited one. It must be a multiple of 90; the result is in [0, 360).
func rotationOf(pdf *model.Context, page types.Dict, inherited types.Object) (int, error) {
	object, own := page.Find(keyRotate)
	if !own {
		object = inherited
	}

	if object == nil {
		return 0, nil
	}

	value, err := pdf.DereferenceNumber(object)
	if err != nil {
		return 0, fmt.Errorf("/%s: %w", keyRotate, err)
	}

	if math.Abs(value) > maxRotateMagnitude || math.Mod(value, quarterTurn) != 0 {
		return 0, fmt.Errorf(causeDetailFormat, errRotationNotRight, value)
	}

	return ((int(value) % fullTurn) + fullTurn) % fullTurn, nil
}

// userUnitOf reads the page's /UserUnit, which is not inherited, and defaults to 1.
func userUnitOf(pdf *model.Context, page types.Dict) (float64, error) {
	object, found := page.Find("UserUnit")
	if !found {
		return defaultUnits, nil
	}

	value, err := pdf.DereferenceNumber(object)
	if err != nil {
		return 0, fmt.Errorf("UserUnit: %w", err)
	}

	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, fmt.Errorf(causeDetailFormat, errUserUnitInvalid, value)
	}

	return value, nil
}
