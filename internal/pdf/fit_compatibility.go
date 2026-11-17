// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/core/diag"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
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
func annotationFittingIssue(c context.Context, s *model.Context, geometries []pageGeometry, plans fittingPlan) (diag.AnnotationIssue, error) {
	for index, geometry := range geometries {
		if !plans.page(index).Decision.Transform {
			continue
		}
		if err := c.Err(); err != nil {
			return diag.AnnotationIssue{}, err
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
			return diag.AnnotationIssue{}, err
		}
		for _, obj := range annots {
			if err := c.Err(); err != nil {
				return diag.AnnotationIssue{}, err
			}
			annot, err := s.DereferenceDict(obj)
			if err != nil {
				return diag.AnnotationIssue{}, err
			}
			if annot == nil {
				continue
			}
			flags := 0
			if annot["F"] != nil {
				v, err := s.DereferenceNumber(annot["F"])
				if err != nil {
					return diag.AnnotationIssue{}, err
				}
				flags = int(v)
			}
			if flags&int(model.AnnHidden) != 0 {
				continue
			} // Hidden annotations are never painted or interactive.
			hasAppearance, err := normalAppearance(s, annot)
			if err != nil {
				return diag.AnnotationIssue{}, err
			}
			subtype, err := s.Dereference(annot["Subtype"])
			if err != nil {
				return diag.AnnotationIssue{}, err
			}
			rect, err := readRectangle(s, annot["Rect"])
			if err != nil {
				return diag.AnnotationIssue{}, err
			}
			issue := func(kind diag.AnnotationIssueKind) diag.AnnotationIssue {
				site := diag.PDFLocation{Page: plans.page(index).Original, Path: "Annots"}
				if ref, ok := obj.(types.IndirectRef); ok {
					site.Object = ref.ObjectNumber.Value()
				}
				name, _ := subtype.(types.Name)
				return diag.AnnotationIssue{Kind: kind, Page: page, Subtype: string(name), Site: site}
			}
			// FreeText has no displayed area on either empty side. Text icons
			// and path annotations can paint despite a zero Rect; do not
			// generalize this exception to those subtypes.
			if subtype == types.Name("FreeText") && (rect.Width() == 0 || rect.Height() == 0) {
				continue
			}
			hitArea := false
			// ReadOnly disables interaction, not painting; Widgets use field flags instead.
			readOnly := flags&int(model.AnnReadOnly) != 0 && subtype != types.Name("Widget")
			if flags&int(model.AnnNoView) == 0 && !readOnly {
				action, err := s.Dereference(annot["A"])
				if err != nil {
					return diag.AnnotationIssue{}, err
				}
				hitArea = subtype == types.Name("Link") || action != nil
				aa, err := s.DereferenceDict(annot["AA"])
				if err != nil {
					return diag.AnnotationIssue{}, err
				}
				for _, trigger := range []string{"E", "X", "D", "U", "Fo", "Bl"} {
					action, err := s.Dereference(aa[trigger])
					if err != nil {
						return diag.AnnotationIssue{}, err
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
				return issue(diag.RotatedAppearanceMissing), nil
			}
			if !hasAppearance && subtype == types.Name("Text") {
				return issue(diag.TextIconAppearanceMissing), nil
			}
			if !hasAppearance && subtype == types.Name("Caret") {
				return issue(diag.CaretAppearanceMissing), nil
			}
			if !hasAppearance && subtype == types.Name("Line") {
				caption, err := s.Dereference(annot["Cap"])
				if err != nil {
					return diag.AnnotationIssue{}, err
				}
				if caption == types.Boolean(true) {
					return issue(diag.CaptionedLineAppearanceMissing), nil
				}
			}
			if !hasAppearance && subtype == types.Name("FreeText") {
				return issue(diag.FreeTextAppearanceMissing), nil
			}
			if rect.Width() == 0 || rect.Height() == 0 {
				continue
			}
			transform, err := annotationTransform(s, annot, *rect, m, 1)
			if err != nil {
				return diag.AnnotationIssue{}, err
			}
			displayed := fitRectangle(*rect, transform)
			if displayed.LL.X < visible.LL.X || displayed.LL.Y < visible.LL.Y || displayed.UR.X > visible.UR.X || displayed.UR.Y > visible.UR.Y {
				if hasAppearance {
					return issue(diag.InteractiveAreaCrossesFrame), nil
				}
				return issue(diag.ArtworkCrossesFrame), nil
			}
		}
	}
	return diag.AnnotationIssue{}, nil
}
