// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package diag

import (
	"fmt"
	"strconv"
)

// AnnotationIssueKind is a closed observed incompatibility, never rendered wording.
type AnnotationIssueKind uint8

// Supported annotation incompatibilities. Zero means no observed issue.
const (
	NoAnnotationIssue AnnotationIssueKind = iota
	RotatedAppearanceMissing
	TextIconAppearanceMissing
	CaretAppearanceMissing
	CaptionedLineAppearanceMissing
	FreeTextAppearanceMissing
	InteractiveAreaCrossesFrame
	ArtworkCrossesFrame
	PDF20AppearanceMissing
)

// AnnotationIssue carries facts observed by the PDF adapter.
type AnnotationIssue struct {
	Kind    AnnotationIssueKind
	Page    int
	Subtype string
	Site    PDFLocation
}

// Present reports whether an incompatibility was established.
func (i AnnotationIssue) Present() bool { return i.Kind != NoAnnotationIssue }

// PageAnnotationMustBeIndirect describes an invalid page annotation slot.
func PageAnnotationMustBeIndirect() Explanation {
	return explain("page_annotation_must_be_indirect", "A page annotation must be an indirect reference to an annotation dictionary.")
}

// FittingAnnotationRefusal owns each complete product sentence and remedy.
func FittingAnnotationRefusal(issue AnnotationIssue) Explanation {
	var text string
	switch issue.Kind {
	case RotatedAppearanceMissing:
		text = fmt.Sprintf("Selected page %d NoRotate annotation has no usable normal appearance; baking rotation cannot preserve viewer-generated bounds and pivot. Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue.Page)
	case TextIconAppearanceMissing:
		text = fmt.Sprintf("Selected page %d Text icon has no usable normal appearance; its rectangle does not bound the viewer-generated icon, so fitting cannot preserve source clipping. Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue.Page)
	case CaretAppearanceMissing:
		text = fmt.Sprintf("Selected page %d Caret has no usable normal appearance; fitting cannot preserve its viewer-generated symbol through rectangle and inset changes alone. Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue.Page)
	case CaptionedLineAppearanceMissing:
		text = fmt.Sprintf("Selected page %d captioned Line has no usable normal appearance; fitting cannot preserve viewer-generated caption text layout. Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue.Page)
	case FreeTextAppearanceMissing:
		text = fmt.Sprintf("Selected page %d FreeText has no usable normal appearance; fitting cannot preserve its text layout. Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue.Page)
	case InteractiveAreaCrossesFrame:
		text = fmt.Sprintf("Selected page %d Interactive annotation hit area crosses the visible source boundary; fitting cannot preserve its clipped interaction region. Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue.Page)
	case ArtworkCrossesFrame:
		text = fmt.Sprintf("Selected page %d annotation without a usable normal appearance crosses the visible source boundary; fitting cannot clip viewer-generated artwork or hit areas. Supply complete normal appearances and keep interactive hit areas inside the source frame, or use original page sizes.", issue.Page)
	case NoAnnotationIssue, PDF20AppearanceMissing:
		panic("not a fitting incompatibility")
	default:
		panic("unknown annotation incompatibility")
	}
	return explain("annotation_fitting", text, strconv.Itoa(int(issue.Kind)), strconv.Itoa(issue.Page), issue.Subtype)
}

// FittingAnnotationViewerDependence describes the NoRotate portability advisory.
func FittingAnnotationViewerDependence(subtype string) Explanation {
	return explain("fitting_annotation_viewer_dependence", fmt.Sprintf(
		"Use original page sizes to keep the source's viewer-dependent behavior, since viewers can differ in the placement or orientation of the %s annotation after fitting bakes page rotation.",
		strconv.QuoteToASCII(subtype)), subtype)
}

// PDF20AnnotationRefusal owns the output-version incompatibility explanation.
func PDF20AnnotationRefusal(issue AnnotationIssue, versionSource string) Explanation {
	if issue.Kind != PDF20AppearanceMissing {
		panic("not a PDF 2.0 appearance incompatibility")
	}
	return explain("pdf20_annotation_appearance", fmt.Sprintf("PDF 2.0 %s annotation requires an appearance dictionary (AP); source %q requires PDF 2.0 output. Supply complete annotation appearances or assemble the affected source separately.", issue.Subtype, versionSource), issue.Subtype, versionSource)
}
