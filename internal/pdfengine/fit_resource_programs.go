// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const fitGroup = "Group"

func (i *fitProgramInspector) xobject(ctx context.Context, scope types.Dict, name string, state *fitGraphicsState) error {
	binding, err := i.operandResource(ctx, scope, keyXObject, name)
	if err != nil {
		return fitResourceError(err)
	}

	object := binding.object

	stream, err := fitReadProgramStream(ctx, i.pdf, object)
	if err != nil {
		return fmt.Errorf("used XObject /%.64s stream is unavailable: %w", name, err)
	}

	kind, err := i.name(ctx, stream.Dict[keySubtype])
	if err != nil {
		return fitResourceError(err)
	}

	switch kind {
	case "Image":
		err = i.paintImage(ctx, stream.Dict, state)
		return fitLocatedResourceError(binding.label(), fitObjectID(object, stream.Dict), err)
	case "Form":
		err = i.form(ctx, object, stream, state, "Do "+binding.label())
		return fitLocatedResourceError(binding.label(), fitObjectID(object, stream.Dict), err)
	default:
		return fmt.Errorf("%w: used XObject /%.64s subtype /%.64s is unsupported", errFitUnsupported, name, kind)
	}
}

func (i *fitProgramInspector) form(
	ctx context.Context,
	object types.Object,
	stream *types.StreamDict,
	state *fitGraphicsState,
	edge string,
) error {
	ownedState := *state
	state = &ownedState

	scope, err := i.programScope(ctx, stream.Dict)
	if err != nil {
		return fitResourceError(err)
	}

	matrix, err := i.resourceMatrix(ctx, stream.Dict, fitMatrix)
	if err != nil {
		return fitResourceError(err)
	}

	state.sourceMatrix, err = fitCompose(state.sourceMatrix, matrix)
	if err != nil {
		return fitResourceError(err)
	}

	state.matrix, err = fitCompose(state.matrix, matrix)
	if err != nil {
		return fitResourceError(err)
	}

	if err = i.resourceBox(ctx, stream.Dict, keyBBox, state); err != nil {
		return fitResourceError(err)
	}

	if err = i.group(ctx, stream.Dict["Group"], scope); err != nil {
		return fitResourceError(err)
	}

	identity := fitObjectID(object, stream.Dict)

	content, err := i.programBytes(ctx, identity, stream)
	if err != nil {
		return fitResourceError(err)
	}

	return i.visit(ctx, identity, content, scope, state, fitShortEdge(edge))
}

func (i *fitProgramInspector) programScope(ctx context.Context, dict types.Dict) (types.Dict, error) {
	object, found := dict[keyResources]
	if !found {
		return i.pageResources, nil
	}

	scope, err := i.pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return nil, fmt.Errorf("invoked program /Resources: %w", err)
	}

	return scope, nil
}

func (i *fitProgramInspector) programBytes(ctx context.Context, identity string, stream *types.StreamDict) ([]byte, error) {
	if cached, found := i.decoded[identity]; found {
		return cached, nil
	}

	content, err := fitDecodeProgram(ctx, stream, min(fitProgramByteLimit, fitSourceProgramByteLimit-i.bytes))
	if err != nil {
		return nil, fitResourceError(err)
	}

	i.bytes += int64(len(content))

	i.decoded[identity] = content

	return content, nil
}

func (i *fitProgramInspector) name(ctx context.Context, object types.Object) (string, error) {
	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return "", fitResourceError(err)
	}

	name, ok := value.(types.Name)
	if !ok {
		return "", fmt.Errorf("%w: active resource name has type %T", errFitUnsupported, value)
	}

	return string(name), nil
}

func fitShortEdge(edge string) string {
	if len(edge) > fitEdgeLabelByteLimit {
		return edge[:fitEdgeLabelByteLimit] + "..."
	}

	return edge
}

func (i *fitProgramInspector) group(ctx context.Context, object types.Object, scope types.Dict) error {
	if object == nil {
		return nil
	}

	dict, err := i.pdf.DereferenceDictContext(ctx, object)
	if err != nil {
		return fitResourceError(err)
	}

	if dict == nil {
		return nil
	}

	if err = i.groupAttributes(ctx, dict); err != nil {
		return fitResourceError(err)
	}

	if raw, found := dict["CS"]; found {
		value, readErr := i.pdf.DereferenceContext(ctx, raw)
		if readErr != nil {
			return fitResourceError(readErr)
		}

		if value != nil {
			_, err = i.blendingSpace(ctx, scope, raw, 0)
		}
	}

	return fitResourceError(err)
}

func (i *fitProgramInspector) integer(ctx context.Context, object types.Object) (int, error) {
	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return 0, fitResourceError(err)
	}

	integer, ok := value.(types.Integer)
	if !ok {
		return 0, fmt.Errorf("%w: active integer has type %T", errFitUnsupported, value)
	}

	return int(integer), nil
}

func (i *fitProgramInspector) groupAttributes(ctx context.Context, dict types.Dict) error {
	if err := i.groupType(ctx, dict[keyType]); err != nil {
		return fitResourceError(err)
	}

	kind, err := i.name(ctx, dict["S"])
	if err != nil {
		return fitResourceError(err)
	}

	if kind != fitTransparency {
		return fmt.Errorf("%w: Group must be Transparency", errFitUnsupported)
	}

	return i.groupFlags(ctx, dict)
}

func (i *fitProgramInspector) groupFlags(ctx context.Context, dict types.Dict) error {
	for _, key := range []string{"I", "K"} {
		if raw, found := dict[key]; found {
			value, readErr := i.pdf.DereferenceContext(ctx, raw)
			if readErr != nil {
				return fitResourceError(readErr)
			}

			if value == nil {
				continue
			}

			if _, ok := value.(types.Boolean); !ok {
				return fmt.Errorf("%w: Group /%s must be boolean", errFitUnsupported, key)
			}
		}
	}

	return nil
}

func (i *fitProgramInspector) groupType(ctx context.Context, object types.Object) error {
	value, err := i.pdf.DereferenceContext(ctx, object)
	if err != nil {
		return fitResourceError(err)
	}

	if value == nil {
		return nil
	}

	name, ok := value.(types.Name)
	if !ok {
		return fmt.Errorf("%w: Group Type must be a name", errFitUnsupported)
	}

	if string(name) != fitGroup {
		return fmt.Errorf("%w: Group Type must be Group", errFitUnsupported)
	}

	return nil
}
