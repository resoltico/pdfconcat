// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	// FitRange captures consecutive source pages with identical original and final geometry.
	FitRange struct {
		First, Last int
		Fit         PageFit
	}
	fitInspector struct {
		pdf               *model.Context
		content           *fitProgramInspector
		pages             map[types.IndirectRef]bool
		destinations      map[fitDestinationKey]types.Object
		annotationObjects map[types.IndirectRef]bool
		annotationArrays  map[types.IndirectRef]bool
		owner             types.IndirectRef
	}
)

const (
	keyNext              = "Next"
	fitDestinationMode   = "Fit"
	keyBleedBox          = "BleedBox"
	keyTrimBox           = "TrimBox"
	keyArtBox            = "ArtBox"
	keyBBox              = "BBox"
	fitKeyFailureFormat  = "fitting /%s: %w"
	fitQuadFailureFormat = "link /QuadPoints: %w"
)

var errFitUnsupported = errors.New("unsupported page fitting")

// inspectPageFits runs on the original graph before validator repairs. Check and import share it;
// no generated page is rendered before all source-known fit refusals are resolved.
func inspectPageFits(ctx context.Context, pdf *model.Context, target PageSize) ([]FitRange, error) {
	inspector := fitInspector{
		pdf:               pdf,
		content:           newFitProgramInspector(pdf),
		pages:             map[types.IndirectRef]bool{},
		destinations:      map[fitDestinationKey]types.Object{},
		annotationObjects: map[types.IndirectRef]bool{},
		annotationArrays:  map[types.IndirectRef]bool{},
	}

	root, err := pdf.PagesContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: page tree: %w", errFitUnsupported, err)
	}

	err = walkPages(ctx, pdf, *root, func(ref types.IndirectRef, _ types.Dict, _ inheritedAttrs) error {
		inspector.pages[ref] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: page identities: %w", errFitUnsupported, err)
	}

	if catalogErr := inspector.catalog(ctx); catalogErr != nil {
		return nil, catalogErr
	}

	var ranges []FitRange

	number := 0
	err = walkPages(ctx, pdf, *root, func(ref types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		number++
		inspector.owner = ref

		fit, fitErr := inspector.page(ctx, page, inherited, target)
		if fitErr != nil {
			return fmt.Errorf("%w: source page %d (object %s): %w", errFitUnsupported, number, ref.PDFString(), fitErr)
		}

		ranges = appendFitRange(ranges, number, fit)

		return nil
	})

	return ranges, err
}

func appendFitRange(ranges []FitRange, number int, fit PageFit) []FitRange {
	if last := len(ranges) - 1; last >= 0 && ranges[last].Fit == fit {
		ranges[last].Last = number
		return ranges
	}

	return append(ranges, FitRange{First: number, Last: number, Fit: fit})
}

func (i *fitInspector) page(ctx context.Context, page types.Dict, inherited inheritedAttrs, target PageSize) (PageFit, error) {
	geometry, err := decomposePage(ctx, i.pdf, page, inherited)
	if err != nil {
		return PageFit{}, err
	}

	fit, err := fitGeometry(geometry, target)
	if err != nil {
		return PageFit{}, err
	}

	for _, check := range []func() error{
		func() error { return i.pageMetadata(ctx, page) },
		func() error { _, boxesErr := i.printBoxes(ctx, page, fit); return boxesErr },
		func() error { return i.annotations(ctx, page, fit) },
		func() error { return i.content.inspectPage(ctx, page, inherited.override(page).resources, fit) },
	} {
		if checkErr := check(); checkErr != nil {
			return PageFit{}, checkErr
		}
	}

	return fit, nil
}

func fitContentError(err error) error {
	if errors.Is(err, model.ErrNoContent) {
		return nil
	}

	return err
}

func (i *fitInspector) nonempty(ctx context.Context, dict types.Dict, key string) error {
	value, err := i.pdf.DereferenceContext(ctx, dict[key])
	if err != nil {
		return fmt.Errorf(fitKeyFailureFormat, key, err)
	}

	if materialFeatureValue(value) {
		return fmt.Errorf("%w: active /%s cannot preserve fitted coordinates or behavior", errFitUnsupported, key)
	}

	return nil
}

func (i *fitInspector) pageMetadata(ctx context.Context, page types.Dict) error {
	// Structure coordinates belong to the omitted catalog tag tree; StructParents is removed by the
	// existing import policy. Retained page-local coordinate or transition state needs explicit proof.
	for _, key := range []string{"AA", "B", "VP", "Measure", "LGIDict", "Thumb", "Trans", "PresSteps", "BoxColorInfo"} {
		if err := i.nonempty(ctx, page, key); err != nil {
			return err
		}
	}

	// Group semantics are checked once by the shared program inspector, even on blank pages.

	return nil
}
