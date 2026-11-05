// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

func normalAppearance(s *model.Context, d types.Dict) (bool, error) {
	ap, err := s.DereferenceDict(d["AP"])
	if err != nil {
		return false, err
	}
	value, err := s.Dereference(ap["N"])
	if err != nil {
		return false, err
	}
	if states, ok := value.(types.Dict); ok {
		state, err := s.Dereference(d["AS"])
		if err != nil {
			return false, err
		}
		name, _ := state.(types.Name)
		value, err = s.Dereference(states[string(name)])
		if err != nil {
			return false, err
		}
	}
	_, ok := value.(types.StreamDict)
	return ok, nil
}

// annotationFittingIssue records only the selected context's incompatibilities.
// Missing-AP path annotations inside the source frame remain supported. A
// fallback renderer cannot be clipped through page-content graphics state, and
// FreeText DA/rich-text layout cannot be preserved by coordinate changes alone.
func annotationFittingIssue(c context.Context, s *model.Context, geometries []pageGeometry) (string, error) {
	for index, geometry := range geometries {
		if err := c.Err(); err != nil {
			return "", err
		}
		page := index + 1
		d, attrs, box := geometry.dict, geometry.attrs, geometry.box

		width, height := box.Width(), box.Height()
		if attrs.Rotate%180 != 0 {
			width, height = height, width
		}
		m, _ := fitMatrix(box, attrs.Rotate, &packet.Size{Width: width, Height: height})
		visible := fitRectangle(box, m)
		annots, err := s.DereferenceArray(d["Annots"])
		if err != nil {
			return "", err
		}
		for _, obj := range annots {
			if err := c.Err(); err != nil {
				return "", err
			}
			annot, err := s.DereferenceDict(obj)
			if err != nil {
				return "", err
			}
			if annot == nil {
				continue
			}
			flags := 0
			if annot["F"] != nil {
				v, err := s.DereferenceNumber(annot["F"])
				if err != nil {
					return "", err
				}
				flags = int(v)
			}
			if flags&int(model.AnnHidden) != 0 {
				continue
			} // Hidden annotations are never painted or interactive.
			hasAppearance, err := normalAppearance(s, annot)
			if err != nil {
				return "", err
			}
			subtype, err := s.Dereference(annot["Subtype"])
			if err != nil {
				return "", err
			}
			rect, err := readRectangle(s, annot["Rect"])
			if err != nil {
				return "", err
			}
			// FreeText has no displayed area on either empty side. Text icons
			// and path annotations can paint despite a zero Rect; do not
			// generalize this exception to those subtypes.
			if subtype == types.Name("FreeText") && (rect.Width() == 0 || rect.Height() == 0) {
				continue
			}
			hitArea := false
			if flags&int(model.AnnNoView) == 0 {
				action, err := s.Dereference(annot["A"])
				if err != nil {
					return "", err
				}
				hitArea = subtype == types.Name("Link") || action != nil
				aa, err := s.DereferenceDict(annot["AA"])
				if err != nil {
					return "", err
				}
				for _, trigger := range []string{"E", "X", "D", "U", "Fo", "Bl"} {
					action, err := s.Dereference(aa[trigger])
					if err != nil {
						return "", err
					}
					if action != nil {
						hitArea = true
					}
				}
			}
			if hasAppearance && !hitArea {
				continue
			}
			if !hasAppearance && attrs.Rotate%360 != 0 && (flags&int(model.AnnNoRotate) != 0 || subtype == types.Name("Text")) {
				return fmt.Sprintf("Selected page %d NoRotate annotation has no usable normal appearance; baking rotation cannot preserve viewer-generated bounds and pivot.", page), nil
			}
			if !hasAppearance && subtype == types.Name("Text") {
				return fmt.Sprintf("Selected page %d Text icon has no usable normal appearance; its rectangle does not bound the viewer-generated icon, so fitting cannot preserve source clipping.", page), nil
			}
			if !hasAppearance && subtype == types.Name("Caret") {
				return fmt.Sprintf("Selected page %d Caret has no usable normal appearance; fitting cannot preserve its viewer-generated symbol through rectangle and inset changes alone.", page), nil
			}
			if !hasAppearance && subtype == types.Name("Line") {
				caption, err := s.Dereference(annot["Cap"])
				if err != nil {
					return "", err
				}
				if caption == types.Boolean(true) {
					return fmt.Sprintf("Selected page %d captioned Line has no usable normal appearance; fitting cannot preserve viewer-generated caption text layout.", page), nil
				}
			}
			if !hasAppearance && subtype == types.Name("FreeText") {
				return fmt.Sprintf("Selected page %d FreeText has no usable normal appearance; fitting cannot preserve its text layout.", page), nil
			}
			if rect.Width() == 0 || rect.Height() == 0 {
				continue
			}
			transform, err := annotationTransform(s, annot, *rect, m, 1)
			if err != nil {
				return "", err
			}
			displayed := fitRectangle(*rect, transform)
			if displayed.LL.X < visible.LL.X || displayed.LL.Y < visible.LL.Y || displayed.UR.X > visible.UR.X || displayed.UR.Y > visible.UR.Y {
				if hasAppearance {
					return fmt.Sprintf("Selected page %d Interactive annotation hit area crosses the visible source boundary; fitting cannot preserve its clipped interaction region.", page), nil
				}
				return fmt.Sprintf("Selected page %d annotation without a usable normal appearance crosses the visible source boundary; fitting cannot clip viewer-generated artwork or hit areas.", page), nil
			}
		}
	}
	return "", nil
}

func fittingAnnotationMessage(issue string) string {
	return issue + " Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes."
}
