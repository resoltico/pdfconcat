// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

// pageGeometry is a resolved, validated view of one leaf at the time of use.
// Inheritance follows the validated Kids traversal, never an untrusted Parent.
type pageGeometry struct {
	pageRef
	attrs model.InheritedPageAttrs
	box   types.Rectangle
	size  packet.Size
	unit  float64
}

// readRectangle interprets PDF's two opposite corners (ISO 32000-1 7.9.5).
// Normalize declarations, not computed intersections: an empty intersection
// must stay empty rather than becoming a new visible rectangle.
func readRectangle(s *model.Context, obj types.Object) (*types.Rectangle, error) {
	a, err := s.DereferenceArray(obj)
	if err != nil {
		return nil, err
	}
	if len(a) != 4 {
		return nil, refuse(diag.SourceInvalid, "A rectangle must contain four coordinates.")
	}
	r, err := s.RectForArray(a)
	if err != nil {
		return nil, err
	}
	return types.NewRectangle(math.Min(r.LL.X, r.UR.X), math.Min(r.LL.Y, r.UR.Y), math.Max(r.LL.X, r.UR.X), math.Max(r.LL.Y, r.UR.Y)), nil
}

func effectivePageBox(attrs *model.InheritedPageAttrs) types.Rectangle {
	box := *attrs.MediaBox
	if crop := attrs.CropBox; crop != nil {
		box.LL.X = math.Max(box.LL.X, crop.LL.X)
		box.LL.Y = math.Max(box.LL.Y, crop.LL.Y)
		box.UR.X = math.Min(box.UR.X, crop.UR.X)
		box.UR.Y = math.Min(box.UR.Y, crop.UR.Y)
	}
	return box
}

func inheritedPageAttrs(s *model.Context, node *pageTreeNode, cache map[*pageTreeNode]model.InheritedPageAttrs) (model.InheritedPageAttrs, error) {
	if node == nil {
		return model.InheritedPageAttrs{}, nil
	}
	if attrs, ok := cache[node]; ok {
		return attrs, nil
	}
	attrs, err := inheritedPageAttrs(s, node.parent, cache)
	if err != nil {
		return attrs, err
	}
	for _, key := range []string{"MediaBox", "CropBox", "Rotate", "Resources"} {
		value, err := s.Dereference(node.dict[key])
		if err != nil {
			return attrs, err
		}
		if value == nil {
			continue
		} // Null inheritable entries behave as absent entries.
		switch key {
		case "MediaBox", "CropBox":
			rect, err := readRectangle(s, value)
			if err != nil {
				return attrs, err
			}
			if key == "MediaBox" {
				attrs.MediaBox = rect
			} else {
				attrs.CropBox = rect
			}
		case "Rotate":
			angle, err := s.DereferenceNumber(value)
			if err != nil {
				return attrs, err
			}
			if math.IsNaN(angle) || math.IsInf(angle, 0) || math.Mod(angle, 90) != 0 {
				return attrs, refuse(diag.SourceInvalid, "Page rotation must be a finite multiple of 90 degrees.")
			}
			attrs.Rotate = int(math.Mod(angle, 360))
		case "Resources":
			attrs.Resources, err = s.DereferenceDict(value)
			if err != nil {
				return attrs, err
			}
		}
	}
	cache[node] = attrs
	return attrs, nil
}

func validPhysicalDimension(n float64) bool { return n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }

// pageGeometries resolves each tree node once. Root PageDict lookups in a loop
// rescan preceding siblings and make a flat document quadratic to inspect.
func pageGeometries(c context.Context, s *model.Context) ([]pageGeometry, error) {
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, err
	}
	result := make([]pageGeometry, 0, len(pages))
	cache := map[*pageTreeNode]model.InheritedPageAttrs{}
	for index, page := range pages {
		if err := c.Err(); err != nil {
			return nil, err
		}
		invalid := func(message string) error {
			finding := diag.New(diag.SourceInvalid, message)
			finding.PDFLocation = &diag.PDFLocation{Object: page.ref.ObjectNumber.Value(), Page: index + 1, Path: "Pages.EffectiveGeometry"}
			return &policyError{finding: finding}
		}
		attrs, err := inheritedPageAttrs(s, page.node, cache)
		if err != nil {
			return nil, err
		}
		if attrs.MediaBox == nil {
			return nil, invalid("Page has no effective MediaBox.")
		}
		box := effectivePageBox(&attrs)
		if !validPhysicalDimension(box.Width()) || !validPhysicalDimension(box.Height()) {
			return nil, invalid("Page's effective CropBox–MediaBox frame must have finite positive dimensions.")
		}
		unit := 1.0
		value, err := s.Dereference(page.dict["UserUnit"])
		if err != nil {
			return nil, err
		}
		switch n := value.(type) {
		case nil:
		case types.Integer:
			unit = float64(n)
		case types.Float:
			unit = float64(n)
		default:
			return nil, invalid("Page UserUnit must be a finite positive number.")
		}
		if !validPhysicalDimension(unit) {
			return nil, invalid("Page UserUnit must be a finite positive number.")
		}
		width, height := box.Width()*unit, box.Height()*unit
		if attrs.Rotate%180 != 0 {
			width, height = height, width
		}
		if !validPhysicalDimension(width) || !validPhysicalDimension(height) {
			return nil, invalid("Page must have finite positive displayed physical dimensions.")
		}
		result = append(result, pageGeometry{pageRef: page, attrs: attrs, box: box, unit: unit, size: packet.Size{Width: width, Height: height}})
	}
	return result, nil
}
