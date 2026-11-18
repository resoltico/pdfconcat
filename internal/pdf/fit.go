// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
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
		(size.Width-scale*upright.Width())/2-scale*upright.LL.X,
		(size.Height-scale*upright.Height())/2-scale*upright.LL.Y)
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

func fitContext(c context.Context, s *model.Context, plans fittingPlan, origins sourceOrigins) error {
	geometries, err := pageGeometries(c, s)
	if err != nil {
		return err
	}
	return fitGeometries(c, s, geometries, plans, origins)
}

// fitGeometries consumes geometry tracked in the current graph revision.
func fitGeometries(c context.Context, s *model.Context, geometries []pageGeometry, plans fittingPlan, origins sourceOrigins) error {
	if err := bindFitting(geometries, plans); err != nil {
		return err
	}
	findings, e := fittingFindings(c, s, geometries, plans, origins)
	if e != nil {
		return e
	}
	for _, d := range findings {
		if d.Severity == diag.Error {
			return &policyError{finding: d}
		}
	}
	if !anyTransform(plans) {
		return c.Err()
	}
	contents, err := capturePageContent(c, s)
	if err != nil {
		return err
	}
	for i, page := range geometries {
		if err := c.Err(); err != nil {
			return err
		}
		if !plans.page(i).Decision.Transform {
			continue
		}
		attrs := page.attrs
		box := page.box
		m, scale := plans.page(i).Matrix, plans.page(i).Scale
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
		target := types.RectForWidthAndHeight(0, 0, plans.page(i).Decision.Output.Width, plans.page(i).Decision.Output.Height)
		page.dict["MediaBox"], page.dict["CropBox"] = target.Array(), target.Array()
		page.dict["Rotate"] = types.Integer(0)
		delete(page.dict, "UserUnit")
		for _, key := range []string{"BleedBox", "TrimBox", "ArtBox"} {
			value, e := s.Dereference(page.dict[key])
			if e != nil {
				return e
			}
			if value != nil {
				delete(page.dict, key)
			}
		}
	}
	s.EnsureVersionForWriting()
	return c.Err()
}
