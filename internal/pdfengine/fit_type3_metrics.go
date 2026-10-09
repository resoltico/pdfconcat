// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func (i *fitProgramInspector) type3Declaration(ctx context.Context,
	instructions []fitInstruction,
	width float64,
	state *fitGraphicsState,
	font types.Dict,
) error {
	if len(instructions) == 0 {
		return fmt.Errorf("%w: used CharProc has no initial d0/d1 metrics", errFitUnsupported)
	}

	first := instructions[0]

	var count int

	switch first.operation.(type) {
	case cs.OpSetCharWidth:
		count = 2
	case cs.OpSetCacheDevice:
		count = 6
	default:
		return fmt.Errorf("%w: first used CharProc operator must be d0 or d1", errFitUnsupported)
	}

	numbers := first.span.Numbers
	if numbers[0].Value != width || numbers[1].Value != 0 {
		return fmt.Errorf("%w: used CharProc wx/wy disagrees with Widths and horizontal displacement", errFitUnsupported)
	}

	if count == fitAffineCoefficients {
		box := [4]float64{numbers[2].Value, numbers[3].Value, numbers[4].Value, numbers[5].Value}
		state.colorRestricted = true

		return i.programBox(box, box, state)
	}

	box, err := i.resourceBoxValues(ctx, font, "FontBBox")
	if err != nil {
		return fitResourceError(err)
	}

	if box == ([4]float64{}) {
		return fitUnknownGlyphDomain(instructions)
	}

	return i.programBox(box, box, state)
}

// A zero FontBBox supplies no domain. Resource paints carry their own Form/image domains;
// other painting requires an actual glyph declaration rather than a fabricated origin bound.
func fitUnknownGlyphDomain(instructions []fitInstruction) error {
	for _, instruction := range instructions {
		switch instruction.operation.(type) {
		case cs.OpFill,
			cs.OpEOFill,
			cs.OpStroke,
			cs.OpCloseStroke,
			cs.OpFillStroke,
			cs.OpEOFillStroke,
			cs.OpCloseFillStroke,
			cs.OpCloseEOFillStroke,
			cs.OpShFill,
			cs.OpShowText,
			cs.OpShowSpaceText,
			cs.OpMoveShowText,
			cs.OpMoveSetShowText:
			return fmt.Errorf(
				"%w: used d0 glyph has unknown ink domain (all-zero FontBBox); "+
					"this painted operation needs d1 bounds or a nonzero declared FontBBox, at byte %d:%d",
				errFitUnsupported,
				instruction.span.Start,
				instruction.span.End,
			)
		default:
		}
	}

	return nil
}
