// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func fitNumbers(s *model.Context, obj types.Object) ([]float64, error) {
	a, err := s.DereferenceArray(obj)
	if err != nil {
		return nil, err
	}
	values := make([]float64, len(a))
	for i, v := range a {
		values[i], err = s.DereferenceNumber(v)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func fitPoints(s *model.Context, obj types.Object, m matrix.Matrix) (types.Array, error) {
	values, err := fitNumbers(s, obj)
	if err != nil {
		return nil, err
	}
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("odd point coordinate count: %d", len(values))
	}
	result := types.Array{}
	for i := 0; i < len(values); i += 2 {
		p := m.Transform(types.Point{X: values[i], Y: values[i+1]})
		result = append(result, types.Float(p.X), types.Float(p.Y))
	}
	return result, nil
}

func fitInsets(s *model.Context, d types.Dict, rect types.Rectangle, m matrix.Matrix) error {
	if d["RD"] == nil {
		return nil
	}
	v, err := fitNumbers(s, d["RD"])
	if err != nil {
		return err
	}
	if len(v) != 4 {
		return fmt.Errorf("invalid RD length: %d", len(v))
	}
	inner := types.NewRectangle(rect.LL.X+v[0], rect.LL.Y+v[3], rect.UR.X-v[2], rect.UR.Y-v[1])
	outer, inside := fitRectangle(rect, m), fitRectangle(*inner, m)
	d["RD"] = types.NewNumberArray(inside.LL.X-outer.LL.X, outer.UR.Y-inside.UR.Y, outer.UR.X-inside.UR.X, inside.LL.Y-outer.LL.Y)
	return nil
}

func fitAnnotationDistances(s *model.Context, d types.Dict, scale float64, subtype string, transform matrix.Matrix) error {
	if subtype == "Line" {
		for _, key := range []string{"LL", "LLE", "LLO"} {
			if d[key] != nil {
				v, err := s.DereferenceNumber(d[key])
				if err != nil {
					return err
				}
				d[key] = types.Float(v * scale)
			}
		}
		if d["CO"] != nil {
			v, err := fitNumbers(s, d["CO"])
			if err != nil {
				return err
			}
			for i := range v {
				v[i] *= scale
			}
			d["CO"] = types.NewNumberArray(v...)
		}
	}
	if d["BS"] != nil {
		bs, err := s.DereferenceDict(d["BS"])
		if err != nil {
			return err
		}
		bs = clonedDirectDict(bs)
		width := 1.0
		if bs["W"] != nil {
			width, err = s.DereferenceNumber(bs["W"])
			if err != nil {
				return err
			}
		}
		bs["W"] = types.Float(width * scale)
		if bs["D"] != nil {
			dash, err := fitNumbers(s, bs["D"])
			if err != nil {
				return err
			}
			for i := range dash {
				dash[i] *= scale
			}
			bs["D"] = types.NewNumberArray(dash...)
		}
		d["BS"] = bs
	} else {
		border, err := s.DereferenceArray(d["Border"])
		if err != nil {
			return err
		}
		if border == nil {
			border = types.NewNumberArray(0, 0, 1)
		}
		border = append(types.Array{}, border...)
		for i := 0; i < 3 && i < len(border); i++ {
			v, err := s.DereferenceNumber(border[i])
			if err != nil {
				return err
			}
			border[i] = types.Float(v * scale)
		}
		if len(border) >= 2 && transform[0][0] == 0 {
			border[0], border[1] = border[1], border[0]
		}
		if len(border) == 4 {
			dash, err := fitNumbers(s, border[3])
			if err != nil {
				return err
			}
			for i := range dash {
				dash[i] *= scale
			}
			border[3] = types.NewNumberArray(dash...)
		}
		d["Border"] = border
	}
	return nil
}

// annotationTransform bakes the displayed NoRotate pivot into both semantic
// geometry and appearances. Keeping them in different spaces breaks fallback
// rendering and later appearance regeneration.
func annotationTransform(s *model.Context, d types.Dict, rect types.Rectangle, m matrix.Matrix, scale float64) (matrix.Matrix, error) {
	flags := 0
	if d["F"] != nil {
		v, err := s.DereferenceNumber(d["F"])
		if err != nil {
			return m, err
		}
		flags = int(v)
	}
	subtype, err := s.Dereference(d["Subtype"])
	if err != nil {
		return m, err
	}
	if flags&int(model.AnnNoRotate) == 0 && subtype != types.Name("Text") {
		return m, nil
	}
	anchor := m.Transform(types.Point{X: rect.LL.X, Y: rect.UR.Y})
	return matrix.CalcTransformMatrix(scale, scale, 0, 1, anchor.X-scale*rect.LL.X, anchor.Y-scale*rect.UR.Y), nil
}

func fitAnnotations(c context.Context, s *model.Context, page, resources types.Dict, box types.Rectangle, m matrix.Matrix, scale float64) error {
	annots, err := s.DereferenceArray(page["Annots"])
	if err != nil {
		return err
	}
	for _, obj := range annots {
		if err := c.Err(); err != nil {
			return err
		}
		d, err := s.DereferenceDict(obj)
		if err != nil {
			return err
		}
		if d == nil {
			continue
		}
		rect, err := readRectangle(s, d["Rect"])
		if err != nil {
			return err
		}

		sub, err := s.Dereference(d["Subtype"])
		if err != nil {
			return err
		}
		subtype, _ := sub.(types.Name)
		appearanceMatrix, err := annotationTransform(s, d, *rect, m, scale)
		if err != nil {
			return err
		}
		clip := box
		// NoRotate (also implicit for Text) pivots about the source upper-left
		// corner, not the upper-left of the enclosing rotated rectangle. NoZoom
		// remains a viewer magnification rule; fitting changes document geometry.
		if appearanceMatrix != m {
			visible := fitRectangle(box, m)
			tx, ty := appearanceMatrix[2][0], appearanceMatrix[2][1]
			clip = *types.NewRectangle((visible.LL.X-tx)/scale, (visible.LL.Y-ty)/scale, (visible.UR.X-tx)/scale, (visible.UR.Y-ty)/scale)
		}
		keys := []string{"QuadPoints"}
		switch subtype {
		case "Line":
			keys = append(keys, "L")
		case "Polygon", "PolyLine":
			keys = append(keys, "Vertices")
		case "FreeText":
			keys = append(keys, "CL")
		}
		for _, key := range keys {
			if d[key] == nil {
				continue
			}
			a, err := fitPoints(s, d[key], appearanceMatrix)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			d[key] = a
		}
		if subtype == "Ink" && d["InkList"] != nil {
			strokes, err := s.DereferenceArray(d["InkList"])
			if err != nil {
				return err
			}
			result := types.Array{}
			for _, stroke := range strokes {
				a, err := fitPoints(s, stroke, appearanceMatrix)
				if err != nil {
					return err
				}
				result = append(result, a)
			}
			d["InkList"] = result
		}
		switch subtype {
		case "Square", "Circle", "FreeText", "Caret":
			if err := fitInsets(s, d, *rect, appearanceMatrix); err != nil {
				return err
			}
		}
		if err := fitAnnotationDistances(s, d, scale, string(subtype), appearanceMatrix); err != nil {
			return err
		}
		if err := fitAppearances(c, s, d, resources, *rect, clip, appearanceMatrix); err != nil {
			return err
		}
		d["Rect"] = fitRectangle(*rect, appearanceMatrix).Array()
	}
	return nil
}

// fitAppearances wraps every AP state independently. The original appearance,
// its Matrix, BBox and resources stay immutable, even when shared across pages.
// ISO 32000-1 12.5.5 maps the transformed BBox into the source annotation Rect.
func fitAppearances(c context.Context, s *model.Context, d, resources types.Dict, rect, clip types.Rectangle, m matrix.Matrix) error {
	ap, err := s.DereferenceDict(d["AP"])
	if err != nil {
		return err
	}
	if ap == nil {
		return nil
	}
	ap = clonedDirectDict(ap)
	for _, key := range []string{"N", "R", "D"} {
		if ap[key] == nil {
			continue
		}
		value, err := s.Dereference(ap[key])
		if err != nil {
			return err
		}
		if states, ok := value.(types.Dict); ok {
			states = clonedDirectDict(states)
			for name, obj := range states {
				wrapped, err := fitAppearance(c, s, obj, resources, rect, clip, m)
				if err != nil {
					return err
				}
				states[name] = wrapped
			}
			ap[key] = states
		} else {
			wrapped, err := fitAppearance(c, s, ap[key], resources, rect, clip, m)
			if err != nil {
				return err
			}
			ap[key] = wrapped
		}
	}
	d["AP"] = ap
	return nil
}

func fitAppearance(c context.Context, s *model.Context, obj types.Object, resources types.Dict, rect, clip types.Rectangle, m matrix.Matrix) (types.Object, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	stream, _, err := s.DereferenceStreamDict(obj)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return obj, nil
	}
	box, err := readRectangle(s, stream.Dict["BBox"])
	if err != nil {
		return nil, err
	}

	originalMatrix := matrix.IdentMatrix
	if stream.Dict["Matrix"] != nil {
		v, err := fitNumbers(s, stream.Dict["Matrix"])
		if err != nil {
			return nil, err
		}
		if len(v) != 6 {
			return nil, fmt.Errorf("invalid appearance matrix length: %d", len(v))
		}
		originalMatrix = matrix.Matrix{{v[0], v[1], 0}, {v[2], v[3], 0}, {v[4], v[5], 1}}
	}
	transformed := fitRectangle(*box, originalMatrix)
	if transformed.Width() == 0 || transformed.Height() == 0 || rect.Width() == 0 || rect.Height() == 0 {
		return obj, nil
	}
	sx, sy := rect.Width()/transformed.Width(), rect.Height()/transformed.Height()
	placement := matrix.CalcTransformMatrix(sx, sy, 0, 1, rect.LL.X-sx*transformed.LL.X, rect.LL.Y-sy*transformed.LL.Y)
	command := fmt.Appendf(nil, "q\n%s %s %s %s re W n\n", number(clip.LL.X), number(clip.LL.Y), number(clip.Width()), number(clip.Height()))
	command = append(command, matrixCommand(placement)...)
	resources, name, err := withFormResource(s, resources, "Original", obj)
	if err != nil {
		return nil, err
	}
	command = fmt.Appendf(command, "/%s Do\nQ\n", name)
	ref, err := addStream(s, command, types.Dict{
		"Type": types.Name("XObject"), "Subtype": types.Name("Form"), "BBox": rect.Array(), "Matrix": matrixArray(m),
		"Resources": resources,
	})
	if err != nil {
		return nil, err
	}
	return *ref, nil
}
