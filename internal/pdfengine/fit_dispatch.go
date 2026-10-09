// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	cs "github.com/benoitkugler/pdf/contentstream"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	fitPaintMode       uint8
	fitResourceBinding struct {
		object     types.Object
		name, kind string
	}
)

const (
	fitPaintFill fitPaintMode = 1 << iota
	fitPaintStroke
	fitPaintBoth = fitPaintFill | fitPaintStroke
)

func (i *fitProgramInspector) execute(
	ctx context.Context,
	instruction fitInstruction,
	scope types.Dict,
	state *fitGraphicsState,
	stack *[]fitGraphicsState,
) error {
	if state.colorRestricted {
		if err := fitRestrictedColor(instruction.operation); err != nil {
			return fitResourceError(err)
		}
	}

	switch instruction.operation.(type) {
	case cs.OpSave, cs.OpRestore, cs.OpConcat:
		return executeGraphics(instruction, state, stack)
	case cs.OpBeginText, cs.OpSetTextMatrix, cs.OpTextMove, cs.OpTextMoveSet, cs.OpTextNextLine:
		return executeTextPosition(instruction, state)
	case cs.OpSetTextLeading, cs.OpSetCharSpacing, cs.OpSetWordSpacing, cs.OpSetHorizScaling, cs.OpSetTextRise, cs.OpSetTextRender:
		return executeTextParameters(instruction, state)
	case cs.OpSetFont:
		return i.executeTextFont(ctx, instruction, scope, state)
	case cs.OpSetFillColorSpace, cs.OpSetStrokeColorSpace:
		return i.executeColorSpace(ctx, instruction, scope, state)
	case cs.OpSetFillColorN,
		cs.OpSetStrokeColorN,
		cs.OpSetFillGray,
		cs.OpSetFillRGBColor,
		cs.OpSetFillCMYKColor,
		cs.OpSetStrokeGray,
		cs.OpSetStrokeRGBColor,
		cs.OpSetStrokeCMYKColor:
		return executeColorSelection(instruction, scope, state)
	case cs.OpBeginImage,
		cs.OpXObject,
		cs.OpSetExtGState,
		cs.OpShFill,
		cs.OpFill,
		cs.OpEOFill,
		cs.OpStroke,
		cs.OpCloseStroke,
		cs.OpFillStroke,
		cs.OpEOFillStroke,
		cs.OpCloseFillStroke,
		cs.OpCloseEOFillStroke:
		return i.executeResourcePaint(ctx, instruction, scope, state)
	case cs.OpShowText, cs.OpMoveShowText, cs.OpMoveSetShowText, cs.OpShowSpaceText:
		return i.executeTextPaint(ctx, instruction, state)
	default:
	}

	return nil
}

func executeGraphics(instruction fitInstruction, state *fitGraphicsState, stack *[]fitGraphicsState) error {
	switch instruction.operation.(type) {
	case cs.OpSave:
		if len(*stack) >= fitGraphicsStackLimit {
			return fmt.Errorf("%w: graphics state stack exceeds %d", errFitUnsupported, fitGraphicsStackLimit)
		}

		*stack = append(*stack, *state)
	case cs.OpRestore:
		if n := len(*stack); n > 0 {
			restored := (*stack)[n-1]
			restored.textMatrix = state.textMatrix
			restored.textLine = state.textLine
			restored.textPositionKnown = state.textPositionKnown
			*state = restored
			*stack = (*stack)[:n-1]
		}
	case cs.OpConcat:
		matrix := fitOriginalMatrix(instruction.span)

		var err error

		state.sourceMatrix, err = fitCompose(state.sourceMatrix, matrix)
		if err != nil {
			return fitResourceError(err)
		}

		state.matrix, err = fitCompose(state.matrix, matrix)

		return fitResourceError(err)
	default:
	}

	return nil
}

func executeTextPosition(instruction fitInstruction, state *fitGraphicsState) error {
	switch instruction.operation.(type) {
	case cs.OpBeginText:
		state.textMatrix = fitIdentityMatrix()
		state.textLine = fitIdentityMatrix()
		state.textPositionKnown = true
	case cs.OpSetTextMatrix:
		matrix := fitOriginalMatrix(instruction.span)

		var err error

		state.textMatrix = matrix
		state.textLine = matrix
		state.textPositionKnown = true
		_, err = fitCompose(state.matrix, matrix)

		return fitResourceError(err)
	case cs.OpTextMove:
		return fitMoveText(state, instruction.span.Numbers[0].Value, instruction.span.Numbers[1].Value)
	case cs.OpTextMoveSet:
		state.leading = -instruction.span.Numbers[1].Value
		return fitMoveText(state, instruction.span.Numbers[0].Value, instruction.span.Numbers[1].Value)
	case cs.OpTextNextLine:
		return fitMoveText(state, 0, -state.leading)
	default:
	}

	return nil
}

func executeTextParameters(instruction fitInstruction, state *fitGraphicsState) error {
	switch instruction.operation.(type) {
	case cs.OpSetTextLeading:
		state.leading = instruction.span.Numbers[0].Value
	case cs.OpSetCharSpacing:
		state.charSpace = instruction.span.Numbers[0].Value
	case cs.OpSetWordSpacing:
		state.wordSpace = instruction.span.Numbers[0].Value
	case cs.OpSetHorizScaling:
		state.horizontal = instruction.span.Numbers[0].Value / fitTextPercentBase
	case cs.OpSetTextRise:
		state.rise = instruction.span.Numbers[0].Value
	case cs.OpSetTextRender:
		mode := instruction.span.Numbers[0].Value
		if mode < 0 || mode > 7 || mode != float64(int(mode)) {
			return fmt.Errorf("%w: invalid text rendering mode", errFitUnsupported)
		}

		state.textRender = int(mode)
	default:
	}

	return nil
}

func (i *fitProgramInspector) executeTextFont(
	ctx context.Context,
	instruction fitInstruction,
	scope types.Dict,
	state *fitGraphicsState,
) error {
	switch operation := instruction.operation.(type) {
	case cs.OpSetFont:
		binding, err := i.operandResource(ctx, scope, keyFont, string(operation.Font))
		if err != nil {
			return fitResourceError(err)
		}

		object := binding.object

		dict, err := i.pdf.DereferenceDictContext(ctx, object)
		if err != nil {
			return fitResourceError(err)
		}

		state.font = fitFontSelection{object: object, identity: fitObjectID(object, dict)}
		state.fontSize = instruction.span.Numbers[0].Value
	default:
	}

	return nil
}

func (i *fitProgramInspector) executeColorSpace(
	ctx context.Context,
	instruction fitInstruction,
	scope types.Dict,
	state *fitGraphicsState,
) error {
	switch operation := instruction.operation.(type) {
	case cs.OpSetFillColorSpace:
		pattern, err := i.patternColorSpace(ctx, scope, string(operation.ColorSpace))
		if err != nil {
			return fitResourceError(err)
		}

		state.fillPattern = pattern
		state.fill = fitPatternSelection{}
	case cs.OpSetStrokeColorSpace:
		pattern, err := i.patternColorSpace(ctx, scope, string(operation.ColorSpace))
		if err != nil {
			return fitResourceError(err)
		}

		state.strokePattern = pattern
		state.stroke = fitPatternSelection{}
	default:
	}

	return nil
}

func executeColorSelection(instruction fitInstruction, scope types.Dict, state *fitGraphicsState) error {
	switch operation := instruction.operation.(type) {
	case cs.OpSetFillColorN:
		selection, err := fitSelectPattern(string(operation.Pattern), scope, state)
		if err != nil {
			return fitResourceError(err)
		}

		state.fill = selection
	case cs.OpSetStrokeColorN:
		selection, err := fitSelectPattern(string(operation.Pattern), scope, state)
		if err != nil {
			return fitResourceError(err)
		}

		state.stroke = selection
	case cs.OpSetFillGray, cs.OpSetFillRGBColor, cs.OpSetFillCMYKColor:
		state.fillPattern = false
		state.fill = fitPatternSelection{}
	case cs.OpSetStrokeGray, cs.OpSetStrokeRGBColor, cs.OpSetStrokeCMYKColor:
		state.strokePattern = false
		state.stroke = fitPatternSelection{}
	default:
	}

	return nil
}

func (i *fitProgramInspector) executeResourcePaint(
	ctx context.Context,
	instruction fitInstruction,
	scope types.Dict,
	state *fitGraphicsState,
) error {
	switch operation := instruction.operation.(type) {
	case cs.OpBeginImage:
		return i.paintInlineImage(ctx, operation, state)
	case cs.OpXObject:
		return i.xobject(ctx, scope, string(operation.XObject), state)
	case cs.OpSetExtGState:
		return i.extGState(ctx, scope, string(operation.Dict), state)
	case cs.OpShFill:
		return i.shading(ctx, scope, string(operation.Shading))
	case cs.OpFill, cs.OpEOFill:
		return i.paintPatterns(ctx, state, fitPaintFill)
	case cs.OpStroke, cs.OpCloseStroke:
		return i.paintPatterns(ctx, state, fitPaintStroke)
	default:
		return i.paintPatterns(ctx, state, fitPaintBoth)
	}
}

func (i *fitProgramInspector) executeTextPaint(ctx context.Context, instruction fitInstruction, state *fitGraphicsState) error {
	switch operation := instruction.operation.(type) {
	case cs.OpShowText:
		return i.showText(ctx, []byte(operation.Text), state)
	case cs.OpMoveShowText:
		if err := fitMoveText(state, 0, -state.leading); err != nil {
			return fitResourceError(err)
		}

		return i.showText(ctx, []byte(operation.Text), state)
	case cs.OpMoveSetShowText:
		state.wordSpace = instruction.span.Numbers[0].Value

		state.charSpace = instruction.span.Numbers[1].Value
		if err := fitMoveText(state, 0, -state.leading); err != nil {
			return fitResourceError(err)
		}

		return i.showText(ctx, []byte(operation.Text), state)
	default:
		return i.showTextArray(ctx, instruction.span, state)
	}
}

func (i *fitProgramInspector) resource(ctx context.Context, scope types.Dict, kind, name string) (fitResourceBinding, error) {
	if i.work >= fitResourceWorkLimit {
		return fitResourceBinding{}, fmt.Errorf("%w: resource lookup work limit exceeded", errFitUnsupported)
	}

	i.work++

	resources, err := i.pdf.DereferenceDictContext(ctx, scope[kind])
	if err != nil {
		return fitResourceBinding{}, fmt.Errorf("resource /%s dictionary: %w", kind, err)
	}

	object, found := resources[name]
	if !found || object == nil {
		return fitResourceBinding{}, fmt.Errorf("%w: used /%s /%.64s resource is absent", errFitUnsupported, kind, name)
	}

	return fitResourceBinding{object: object, name: name, kind: kind}, nil
}

// operandResource decodes the content parser's raw name spelling at its representation boundary.
func (i *fitProgramInspector) operandResource(ctx context.Context, scope types.Dict, kind, rawName string) (fitResourceBinding, error) {
	name, err := types.DecodeName(rawName)
	if err != nil {
		return fitResourceBinding{}, fmt.Errorf("resource /%.64s name: %w", rawName, err)
	}

	return i.resource(ctx, scope, kind, name)
}

func (i *fitProgramInspector) patternColorSpace(ctx context.Context, scope types.Dict, name string) (bool, error) {
	decoded, err := types.DecodeName(name)
	if err != nil {
		return false, fitResourceError(err)
	}

	if decoded == fitPattern {
		return true, nil
	}

	if decoded == "DeviceGray" || decoded == fitDeviceRGB || decoded == "DeviceCMYK" {
		return false, nil
	}

	binding, err := i.resource(ctx, scope, fitColorSpace, decoded)
	if err != nil {
		return false, fitResourceError(err)
	}

	object := binding.object

	object, err = i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return false, fitResourceError(err)
	}

	if array, ok := object.(types.Array); ok && len(array) > 0 {
		kind, readErr := i.name(ctx, array[0])
		return kind == fitPattern, fitResourceError(readErr)
	}

	return false, nil
}

func (i *fitProgramInspector) paintPatterns(ctx context.Context, state *fitGraphicsState, mode fitPaintMode) error {
	if mode&fitPaintFill != 0 && state.fillPattern {
		if err := i.paintPatternSelection(ctx, &state.fill, state); err != nil {
			return fitResourceError(err)
		}
	}

	if mode&fitPaintStroke != 0 && state.strokePattern {
		return i.paintPatternSelection(ctx, &state.stroke, state)
	}

	return nil
}

func (binding fitResourceBinding) label() string { return "/" + binding.kind + " /" + binding.name }

func (i *fitProgramInspector) paintPatternSelection(ctx context.Context, selection *fitPatternSelection, state *fitGraphicsState) error {
	if selection.name == "" {
		return fmt.Errorf("%w: painted pattern color has no selected pattern", errFitUnsupported)
	}

	return i.pattern(ctx, *selection, state)
}
