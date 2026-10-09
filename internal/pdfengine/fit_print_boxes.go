// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func (i *fitInspector) printBoxes(ctx context.Context, page types.Dict, fit PageFit) (map[string][4]float64, error) {
	boxes := make(map[string][4]float64)
	target := [4]float64{0, 0, fit.Target.Width, fit.Target.Height}

	for _, key := range []string{keyBleedBox, keyTrimBox, keyArtBox} {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("inspect print boxes: %w", err)
		}

		box, found, err := boxOf(ctx, i.pdf, page, nil, key)
		if err != nil {
			return nil, fmt.Errorf("%w: /%s: %w", errFitUnsupported, key, err)
		}

		if !found {
			continue
		}

		original := rectangleArray(box)
		if !withinFitBox(original, fit.Media) {
			return nil, fmt.Errorf("%w: /%s extends outside original /MediaBox", errFitUnsupported, key)
		}

		transformed, err := fitTransformedBox(fit.Matrix, original)
		if err != nil {
			return nil, fmt.Errorf(fitKeyFailureFormat, key, err)
		}

		if !withinFitBox(transformed, target) {
			return nil, fmt.Errorf("%w: /%s transforms outside target sheet", errFitUnsupported, key)
		}

		boxes[key] = transformed
	}

	if err := consistentFitBoxes(boxes); err != nil {
		return nil, err
	}

	return boxes, nil
}

func consistentFitBoxes(boxes map[string][4]float64) error {
	for _, relation := range [][2]string{{keyTrimBox, keyBleedBox}, {keyArtBox, keyBleedBox}, {keyArtBox, keyTrimBox}} {
		inner, hasInner := boxes[relation[0]]

		outer, hasOuter := boxes[relation[1]]
		if hasInner && hasOuter && !withinFitBox(inner, outer) {
			return fmt.Errorf("%w: /%s contradicts /%s", errFitUnsupported, relation[0], relation[1])
		}
	}

	return nil
}
