// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

func fitMatrix(box types.Rectangle, rotation int, size *packet.Size) (matrix.Matrix, float64) {
	m := matrix.IdentMatrix
	switch (rotation%360 + 360) % 360 {
	case 90:
		m = matrix.Matrix{{0, -1, 0}, {1, 0, 0}, {0, 0, 1}}
	case 180:
		m = matrix.Matrix{{-1, 0, 0}, {0, -1, 0}, {0, 0, 1}}
	case 270:
		m = matrix.Matrix{{0, 1, 0}, {-1, 0, 0}, {0, 0, 1}}
	}
	upright := fitRectangle(box, m)
	scale := math.Min(size.Width/upright.Width(), size.Height/upright.Height())
	placement := matrix.CalcTransformMatrix(scale, scale, 0, 1,
		box.LL.X+(size.Width-scale*upright.Width())/2-scale*upright.LL.X,
		box.LL.Y+(size.Height-scale*upright.Height())/2-scale*upright.LL.Y)
	return m.Multiply(placement), scale
}

func fitRectangle(r types.Rectangle, m matrix.Matrix) *types.Rectangle {
	points := types.QuadLiteral{
		P1: m.Transform(r.LL), P2: m.Transform(types.Point{X: r.UR.X, Y: r.LL.Y}),
		P3: m.Transform(r.UR), P4: m.Transform(types.Point{X: r.LL.X, Y: r.UR.Y}),
	}
	return points.EnclosingRectangle(0)
}

func matrixArray(m matrix.Matrix) types.Array {
	return types.NewNumberArray(m[0][0], m[0][1], m[1][0], m[1][1], m[2][0], m[2][1])
}

func matrixCommand(m matrix.Matrix) []byte {
	return fmt.Appendf(nil, "%s %s %s %s %s %s cm\n", number(m[0][0]), number(m[0][1]), number(m[1][0]), number(m[1][1]), number(m[2][0]), number(m[2][1]))
}

func fitContext(c context.Context, s *model.Context, size *packet.Size) error {
	geometries, err := pageGeometries(c, s)
	if err != nil {
		return err
	}
	issue, err := annotationFittingIssue(c, s, geometries)
	if err != nil {
		return err
	}
	if issue != "" {
		return refuse(diag.FitAnnotationUnsupported, fittingAnnotationMessage(issue))
	}
	contents, err := capturePageContent(c, s)
	if err != nil {
		return err
	}
	for i, page := range geometries {
		if err := c.Err(); err != nil {
			return err
		}
		attrs := page.attrs
		box := page.box
		m, scale := fitMatrix(box, attrs.Rotate, size)
		data, err := joinedPageContent(c, s, contents[i], i+1)
		if err != nil {
			return err
		}
		// A Form establishes the original default coordinate space for patterns and
		// an unescapable BBox clip. Do not rewrite shared/nested resources or matrices.
		extra := types.Dict{"Type": types.Name("XObject"), "Subtype": types.Name("Form"), "BBox": box.Array(), "Resources": clonedDirectDict(attrs.Resources)}
		if group := page.dict["Group"]; group != nil {
			extra["Group"] = group.Clone()
		}
		form, err := addStream(s, data, extra)
		if err != nil {
			return err
		}
		if err = fitStructure(c, s, page.ref, *form, m); err != nil {
			return err
		}
		resources, name, err := withFormResource(s, attrs.Resources, "PCSource", *form)
		if err != nil {
			return err
		}
		command := append([]byte("q\n"), matrixCommand(m)...)
		command = fmt.Appendf(command, "/%s Do\nQ\n", name)
		content, err := addStream(s, command, nil)
		if err != nil {
			return err
		}
		if err := fitAnnotations(c, s, page.dict, attrs.Resources, box, m, scale); err != nil {
			return fmt.Errorf("fit page %d annotations: %w", i+1, err)
		}
		page.dict["Resources"] = resources
		page.dict["Contents"] = *content
		target := types.RectForWidthAndHeight(box.LL.X, box.LL.Y, size.Width, size.Height)
		page.dict["MediaBox"], page.dict["CropBox"] = target.Array(), target.Array()
		page.dict["Rotate"] = types.Integer(0)
		delete(page.dict, "UserUnit")
	}
	s.EnsureVersionForWriting()
	return c.Err()
}
