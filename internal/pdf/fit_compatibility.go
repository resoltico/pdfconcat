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

// fittingAnnotationFindings observes the selected context's annotation compatibility and advisories.
// Missing-AP path annotations inside the source frame remain supported. A
// fallback renderer cannot be clipped through page-content graphics state, and
// FreeText DA/rich-text layout cannot be preserved by coordinate changes alone.
func fittingAnnotationFindings(c context.Context, s *model.Context, geometries []pageGeometry, plans fittingPlan) (diag.AnnotationIssue, []diag.Diagnostic, error) {
	var warnings []diag.Diagnostic
	for index, geometry := range geometries {
		if !plans.page(index).Decision.Transform {
			continue
		}
		if err := c.Err(); err != nil {
			return diag.AnnotationIssue{}, nil, err
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
			return diag.AnnotationIssue{}, nil, err
		}
		for annotIndex, obj := range annots {
			if err := c.Err(); err != nil {
				return diag.AnnotationIssue{}, nil, err
			}
			annot, err := s.DereferenceDict(obj)
			if err != nil {
				return diag.AnnotationIssue{}, nil, err
			}
			if annot == nil {
				continue
			}
			flags := 0
			if annot["F"] != nil {
				v, err := s.DereferenceNumber(annot["F"])
				if err != nil {
					return diag.AnnotationIssue{}, nil, err
				}
				flags = int(v)
			}
			if flags&int(model.AnnHidden) != 0 {
				continue
			} // Hidden annotations are never painted or interactive.
			hasAppearance, err := normalAppearance(s, annot)
			if err != nil {
				return diag.AnnotationIssue{}, nil, err
			}
			subtype, err := nameValue(s, annot["Subtype"])
			if err != nil {
				return diag.AnnotationIssue{}, nil, err
			}
			rect, err := readRectangle(s, annot["Rect"])
			if err != nil {
				return diag.AnnotationIssue{}, nil, err
			}
			issue := func(kind diag.AnnotationIssueKind) diag.AnnotationIssue {
				site := diag.PDFLocation{Page: plans.page(index).Original, Path: "Annots"}
				if ref, ok := obj.(types.IndirectRef); ok {
					site.Object = ref.ObjectNumber.Value()
				}
				return diag.AnnotationIssue{Kind: kind, Page: page, Subtype: string(subtype), Site: site}
			}
			// FreeText has no displayed area on either empty side. Text icons
			// and path annotations can paint despite a zero Rect; do not
			// generalize this exception to those subtypes.
			if subtype == types.Name("FreeText") && (rect.Width() == 0 || rect.Height() == 0) {
				continue
			}
			hitArea := false
			canShowOnScreen := flags&int(model.AnnNoView) == 0 || flags&int(model.AnnToggleNoView) != 0
			// ReadOnly disables interaction, not painting; Widgets use field flags instead.
			readOnly := flags&int(model.AnnReadOnly) != 0 && subtype != types.Name("Widget")
			if canShowOnScreen && !readOnly {
				action, err := s.Dereference(annot["A"])
				if err != nil {
					return diag.AnnotationIssue{}, nil, err
				}
				hitArea = subtype == types.Name("Link") || action != nil
				aa, err := s.DereferenceDict(annot["AA"])
				if err != nil {
					return diag.AnnotationIssue{}, nil, err
				}
				for _, trigger := range []string{"E", "X", "D", "U", "Fo", "Bl"} {
					action, err := s.Dereference(aa[trigger])
					if err != nil {
						return diag.AnnotationIssue{}, nil, err
					}
					if action != nil {
						hitArea = true
					}
				}
			}
			rotatedNoRotate := attrs.Rotate%360 != 0 && annotationNoRotate(flags, subtype)
			if !hasAppearance && rotatedNoRotate {
				return issue(diag.RotatedAppearanceMissing), warnings, nil
			}
			if !hasAppearance && subtype == types.Name("Text") {
				return issue(diag.TextIconAppearanceMissing), warnings, nil
			}
			if !hasAppearance && subtype == types.Name("Caret") {
				return issue(diag.CaretAppearanceMissing), warnings, nil
			}
			if !hasAppearance && subtype == types.Name("Line") {
				caption, err := s.Dereference(annot["Cap"])
				if err != nil {
					return diag.AnnotationIssue{}, nil, err
				}
				if caption == types.Boolean(true) {
					return issue(diag.CaptionedLineAppearanceMissing), warnings, nil
				}
			}
			if !hasAppearance && subtype == types.Name("FreeText") {
				return issue(diag.FreeTextAppearanceMissing), warnings, nil
			}
			if (!hasAppearance || hitArea) && rect.Width() != 0 && rect.Height() != 0 {
				transform, err := annotationTransform(s, annot, *rect, m, 1)
				if err != nil {
					return diag.AnnotationIssue{}, nil, err
				}
				displayed := fitRectangle(*rect, transform)
				if displayed.LL.X < visible.LL.X || displayed.LL.Y < visible.LL.Y || displayed.UR.X > visible.UR.X || displayed.UR.Y > visible.UR.Y {
					if hasAppearance {
						return issue(diag.InteractiveAreaCrossesFrame), warnings, nil
					}
					return issue(diag.ArtworkCrossesFrame), warnings, nil
				}
			}
			if hasAppearance && rotatedNoRotate && (canShowOnScreen || flags&int(model.AnnPrint) != 0) {
				site := sourceSite{ref: geometry.ref, page: plans.page(index).Original}.key("Annots").index(annotIndex).anchor(obj, sourceOrigins{})
				finding := diag.Report(diag.FitAnnotationViewerDependent, diag.FittingAnnotationViewerDependence(string(subtype)))
				finding.PDFLocation = site.location()
				warnings = append(warnings, finding)
			}
		}
	}
	return diag.AnnotationIssue{}, warnings, nil
}
