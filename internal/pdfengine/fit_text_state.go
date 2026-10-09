// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/benoitkugler/pdf/reader/parser"
	tokenizer "github.com/benoitkugler/pstokenizer"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func fitMoveText(state *fitGraphicsState, x, y float64) error {
	matrix, err := fitCompose(state.textLine, Affine{1, 0, 0, 1, x, y})
	if err != nil {
		return fitResourceError(err)
	}

	state.textLine = matrix
	state.textMatrix = matrix
	state.textPositionKnown = true

	return nil
}

func fitAdvanceText(state *fitGraphicsState, x, y float64) error {
	matrix, err := fitCompose(state.textMatrix, Affine{1, 0, 0, 1, x, y})
	if err != nil {
		return fitResourceError(err)
	}

	state.textMatrix = matrix

	return nil
}

func (i *fitProgramInspector) showTextArray(ctx context.Context, span parser.ContentSpan, state *fitGraphicsState) error {
	numeric := 0

	for _, token := range span.Tokens {
		switch token.Kind {
		case tokenizer.String, tokenizer.StringHex:
			if err := i.showText(ctx, token.Value, state); err != nil {
				return fitResourceError(err)
			}
		case tokenizer.Integer, tokenizer.Float:
			distance := -span.Numbers[numeric].Value / fontMetricScale * state.fontSize * state.horizontal

			numeric++

			advanceErr := fitAdvanceText(state, distance, 0)
			if advanceErr != nil {
				return fitResourceError(advanceErr)
			}
		case tokenizer.EOF,
			tokenizer.Name,
			tokenizer.StartArray,
			tokenizer.EndArray,
			tokenizer.StartDic,
			tokenizer.EndDic,
			tokenizer.Other,
			tokenizer.StartProc,
			tokenizer.EndProc,
			tokenizer.CharString:
			continue
		default:
		}
	}

	return nil
}

func (i *fitProgramInspector) type3Width(ctx context.Context, dict types.Dict, code byte) (float64, error) {
	first, err := i.integer(ctx, dict["FirstChar"])
	if err != nil {
		return 0, fitResourceError(err)
	}

	last, err := i.integer(ctx, dict["LastChar"])
	if err != nil {
		return 0, fitResourceError(err)
	}

	if first < 0 || last > 255 || first > last {
		return 0, fmt.Errorf("%w: shown Type3 character range is invalid", errFitUnsupported)
	}

	widths, err := i.pdf.DereferenceArrayContext(ctx, dict["Widths"])
	if err != nil {
		return 0, fitResourceError(err)
	}

	if len(widths) != last-first+1 {
		return 0, fmt.Errorf("%w: shown Type3 /Widths does not match character range", errFitUnsupported)
	}

	if int(code) < first || int(code) > last {
		return 0, nil
	}

	width, err := i.pdf.DereferenceNumberContext(ctx, widths[int(code)-first])
	if err != nil {
		return 0, fitResourceError(err)
	}

	if !finiteAppearanceNumber(width) {
		return 0, fmt.Errorf("%w: shown Type3 width is not finite", errFitUnsupported)
	}

	return width, nil
}

func fitType3Advance(
	state *fitGraphicsState,
	matrix Affine,
	width float64,
	code byte,
) error {
	x := matrix[0]*width*state.fontSize + state.charSpace
	if code == fitSpaceCode {
		x += state.wordSpace
	}

	x *= state.horizontal

	return fitAdvanceText(state, x, 0)
}
