// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func fitRestrictedColor(operation cs.Operation) error {
	switch operation.(type) {
	case cs.OpSetFillColorSpace,
		cs.OpSetStrokeColorSpace,
		cs.OpSetFillColor,
		cs.OpSetStrokeColor,
		cs.OpSetFillColorN,
		cs.OpSetStrokeColorN,
		cs.OpSetFillGray,
		cs.OpSetStrokeGray,
		cs.OpSetFillRGBColor,
		cs.OpSetStrokeRGBColor,
		cs.OpSetFillCMYKColor,
		cs.OpSetStrokeCMYKColor,
		cs.OpShFill:
		return fmt.Errorf(
			"%w: strict fitting policy refuses an executed color operation in an uncolored pattern or d1 glyph",
			errFitUnsupported,
		)
	default:
		return nil
	}
}

func fitRestrictedExtGState(dict types.Dict) error {
	for _, key := range []string{"TR", "TR2", "BG", "BG2", "UCR", "UCR2", "HT", "RI"} {
		if value, found := dict[key]; found && materialFeatureValue(value) {
			return fmt.Errorf("%w: strict fitting color restriction refuses applied ExtGState /%s", errFitUnsupported, key)
		}
	}

	return nil
}

func (i *fitProgramInspector) paintImage(ctx context.Context, dict types.Dict, state *fitGraphicsState) error {
	stencil := false

	if raw, found := dict["ImageMask"]; found {
		value, err := i.pdf.DereferenceContext(ctx, raw)
		if err != nil {
			return fmt.Errorf("used image /ImageMask: %w", err)
		}

		flag, ok := value.(types.Boolean)
		if !ok {
			return fmt.Errorf("%w: used image /ImageMask must be boolean", errFitUnsupported)
		}

		stencil = bool(flag)
	}

	if err := i.programBox([4]float64{0, 0, 1, 1}, [4]float64{0, 0, 1, 1}, state); err != nil {
		return fitResourceError(err)
	}

	if stencil {
		return i.paintPatterns(ctx, state, fitPaintFill)
	}

	if state.colorRestricted {
		return fmt.Errorf("%w: strict fitting color restriction refuses a painted ordinary image", errFitUnsupported)
	}

	return nil
}

func (i *fitProgramInspector) paintInlineImage(ctx context.Context, image cs.OpBeginImage, state *fitGraphicsState) error {
	if err := i.programBox([4]float64{0, 0, 1, 1}, [4]float64{0, 0, 1, 1}, state); err != nil {
		return fitResourceError(err)
	}

	if image.Image.ImageMask {
		return i.paintPatterns(ctx, state, fitPaintFill)
	}

	if state.colorRestricted {
		return fmt.Errorf("%w: strict fitting color restriction refuses a painted ordinary inline image", errFitUnsupported)
	}

	return nil
}
