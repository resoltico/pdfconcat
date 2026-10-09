// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// readImport applies fitting in the imported context before pool merging and page cloning.
// It repeats raw preflight on the immutable captured bytes rather than trusting caller-supplied facts.
func (p *pool) readImport(ctx context.Context, source int, path string, target *PageSize) (*model.Context, error) {
	if target == nil {
		return p.engine.readContext(ctx, source, path)
	}

	var fits []FitRange

	observe := func(ctx context.Context, pdf *model.Context) ([]SourceFeature, error) {
		var err error

		fits, err = inspectPageFits(ctx, pdf, *target)

		return nil, err
	}

	document, err := p.engine.readDocument(ctx, source, path, observe)
	if err != nil {
		return nil, fittingSourceReadError(ctx, source, path, err)
	}

	if err = applyPageFits(ctx, document.pdf, fits); err != nil {
		if cancelErr := canceled(ctx, source, path); cancelErr != nil {
			return nil, cancelErr
		}

		return nil, newError(CodeFitUnsupported, source, path, err)
	}

	return document.pdf, nil
}

func applyPageFits(ctx context.Context, pdf *model.Context, ranges []FitRange) error {
	root, err := pdf.PagesContext(ctx)
	if err != nil {
		return fmt.Errorf("fit imported page tree: %w", err)
	}

	inspector := fitInspector{pdf: pdf}
	number, interval := 0, 0

	err = walkPages(ctx, pdf, *root, func(_ types.IndirectRef, page types.Dict, inherited inheritedAttrs) error {
		number++
		if interval < len(ranges) && number > ranges[interval].Last {
			interval++
		}

		if interval >= len(ranges) || number < ranges[interval].First || number > ranges[interval].Last {
			return fmt.Errorf("%w: missing captured fit for source page %d", errFitUnsupported, number)
		}

		fit := ranges[interval].Fit
		if fitErr := inspector.transformObjects(ctx, page, fit); fitErr != nil {
			return fmt.Errorf("source page %d: %w", number, fitErr)
		}

		if contentErr := fitPageContent(ctx, pdf, page, inherited, fit); contentErr != nil {
			return fmt.Errorf("source page %d content: %w", number, contentErr)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("fit imported pages: %w", err)
	}

	if len(ranges) == 0 || number != ranges[len(ranges)-1].Last {
		return fmt.Errorf("%w: page count changed after fit inspection", errFitUnsupported)
	}

	return nil
}

func (i *fitInspector) transformObjects(ctx context.Context, page types.Dict, fit PageFit) error {
	boxes, err := i.printBoxes(ctx, page, fit)
	if err != nil {
		return err
	}

	for key, box := range boxes {
		page[key] = fitNumberArray(box[:])
	}

	annotations, err := i.pdf.DereferenceArrayContext(ctx, page["Annots"])
	if err != nil {
		return fmt.Errorf("fit /Annots: %w", err)
	}

	for _, object := range annotations {
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("fit annotation canceled: %w", err)
		}

		annotation, readErr := i.pdf.DereferenceDictContext(ctx, object)
		if readErr != nil {
			return fmt.Errorf("fit annotation object %s: %w", fitObjectIdentity(object), readErr)
		}

		hotspot, readErr := i.linkCoordinates(ctx, annotation, fit)
		if readErr != nil {
			return readErr
		}

		annotation["Rect"] = fitNumberArray(hotspot.rect[:])
		if len(hotspot.quad) > 0 {
			annotation[keyQuadPoints] = fitNumberArray(hotspot.quad)
		}
	}

	return nil
}
