// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type fitType3Context struct {
	dict, procs, scope types.Dict
	encoding           simpleencodings.Encoding
	matrix             Affine
}

func (i *fitProgramInspector) showText(ctx context.Context, text []byte, state *fitGraphicsState) error {
	if len(text) == 0 {
		return nil
	}

	if state.font.object == nil {
		return fmt.Errorf("%w: shown text has no resolvable incoming font", errFitUnsupported)
	}

	dict, err := i.pdf.DereferenceDictContext(ctx, state.font.object)
	if err != nil {
		return fitResourceError(err)
	}

	kind, err := i.name(ctx, dict[keySubtype])
	if err != nil {
		return fitResourceError(err)
	}

	if kind != "Type3" {
		return i.showSimpleText(ctx, dict, kind, text, state)
	}

	return i.showType3Text(ctx, dict, text, state)
}

func (i *fitProgramInspector) showType3Text(ctx context.Context, dict types.Dict, text []byte, state *fitGraphicsState) error {
	if !state.textPositionKnown {
		return fmt.Errorf(
			"%w: shown Type3 call-site position depends on unresolved preceding non-Type3 text advance; "+
				"explicit Tm or line positioning is required",
			errFitUnsupported,
		)
	}

	font, err := i.type3Context(ctx, dict)
	if err != nil {
		return fitResourceError(err)
	}

	for _, code := range text {
		if err = i.step(ctx); err != nil {
			return fitResourceError(err)
		}

		width, e := i.type3Width(ctx, dict, code)
		if e != nil {
			return fitResourceError(e)
		}

		if e = i.showType3Glyph(ctx, font, code, width, state); e != nil {
			return fitResourceError(e)
		}

		if e = fitType3Advance(state, font.matrix, width, code); e != nil {
			return fitResourceError(e)
		}
	}

	return nil
}

func (i *fitProgramInspector) type3Context(ctx context.Context, dict types.Dict) (*fitType3Context, error) {
	font := &fitType3Context{}

	var err error

	font.dict = dict

	font.encoding, err = i.type3Encoding(ctx, dict["Encoding"])
	if err != nil {
		return font, fitResourceError(err)
	}

	font.procs, err = i.pdf.DereferenceDictContext(ctx, dict["CharProcs"])
	if err != nil {
		return font, fitResourceError(err)
	}

	font.scope, err = i.programScope(ctx, dict)
	if err != nil {
		return font, fitResourceError(err)
	}

	if dict["FontMatrix"] == nil {
		return font, fmt.Errorf("%w: used Type3 font requires FontMatrix", errFitUnsupported)
	}

	font.matrix, err = i.resourceMatrix(ctx, dict, "FontMatrix")

	return font, fitResourceError(err)
}

func (i *fitProgramInspector) showType3Glyph(
	ctx context.Context,
	font *fitType3Context,
	code byte,
	width float64,
	state *fitGraphicsState,
) error {
	name := font.encoding[code]
	if name == "" {
		return nil
	}

	object, found := font.procs[name]
	if !found {
		return nil
	}

	stream, err := fitReadProgramStream(ctx, i.pdf, object)
	if err != nil {
		return fitResourceError(err)
	}

	state, err = fitGlyphPlacement(state, font)
	if err != nil {
		return fitResourceError(err)
	}

	identity := fitObjectID(object, stream.Dict)

	content, err := i.programBytes(ctx, identity, stream)
	if err != nil {
		return fitResourceError(err)
	}

	instructions, err := i.parse(ctx, identity, content, font.scope)
	if err != nil {
		return fitResourceError(err)
	}

	if err = i.type3Declaration(ctx, instructions, width, state, font.dict); err != nil {
		return fmt.Errorf("Type3 /%.64s object %s declaration: %w", name, identity, err)
	}

	return i.visit(ctx, identity, content, font.scope, state, fitShortEdge("show Type3 /"+name))
}

func (i *fitProgramInspector) type3Encoding(ctx context.Context, object types.Object) (simpleencodings.Encoding, error) {
	if object == nil {
		return simpleencodings.Encoding{}, fmt.Errorf("%w: shown Type3 font has no Encoding", errFitUnsupported)
	}

	encoding := simpleencodings.Encoding{}

	return i.simpleEncoding(ctx, object, &encoding)
}

func (i *fitProgramInspector) showSimpleText(
	ctx context.Context,
	dict types.Dict,
	kind string,
	text []byte,
	state *fitGraphicsState,
) error {
	fill := state.textRender == 0 || state.textRender == 2 || state.textRender == 4 || state.textRender == fitTextFillStrokeClip

	stroke := state.textRender == 1 || state.textRender == 2 || state.textRender == 5 || state.textRender == fitTextFillStrokeClip

	var mode fitPaintMode
	if fill {
		mode |= fitPaintFill
	}

	if stroke {
		mode |= fitPaintStroke
	}

	if err := i.paintPatterns(ctx, state, mode); err != nil {
		return fitResourceError(err)
	}

	return i.advanceSimpleText(ctx, dict, kind, text, state)
}

func fitGlyphPlacement(state *fitGraphicsState, font *fitType3Context) (*fitGraphicsState, error) {
	ownedState := *state
	state = &ownedState

	emitted, err := fitGlyphMatrix(state.matrix, state.textMatrix, font.matrix, state.fontSize, state.horizontal, state.rise)
	if err != nil {
		return state, fitResourceError(err)
	}

	original, err := fitGlyphMatrix(
		state.sourceMatrix,
		state.textMatrix,
		font.matrix,
		state.fontSize,
		state.horizontal,
		state.rise,
	)
	if err != nil {
		return state, fitResourceError(err)
	}

	state.matrix = emitted
	state.sourceMatrix = original

	return state, nil
}

func fitGlyphMatrix(current, text, font Affine, size, horizontal, rise float64) (Affine, error) {
	matrix, err := fitCompose(current, text)
	if err != nil {
		return Affine{}, fitResourceError(err)
	}

	matrix, err = fitCompose(matrix, Affine{size * horizontal, 0, 0, size, 0, rise})
	if err != nil {
		return Affine{}, fitResourceError(err)
	}

	return fitCompose(matrix, font)
}
