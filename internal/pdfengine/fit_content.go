// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const fitContentName = "FittedSource"

// fitPageContent places the original logical content in an isolated Form XObject. Its BBox enforces
// the original visible boundary independently of source q/Q balance. The ordinary Form retains
// original resources; the output page retains the original single blending context.
func fitPageContent(ctx context.Context, pdf *model.Context, page types.Dict, inherited inheritedAttrs, fit PageFit) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fit content: %w", err)
	}

	content, err := fitLogicalContentLimit(ctx, pdf, page, fitProgramByteLimit)
	if err != nil && !errors.Is(err, model.ErrNoContent) {
		return fmt.Errorf("fit logical content: %w", err)
	}

	resources := inherited.override(page).resources
	if resources == nil {
		resources = types.Dict{}
	}

	form, err := fitForm(ctx, pdf, content, resources, fit.Visible)
	if err != nil {
		return err
	}

	outputResources, wrapperName, err := fittedPageResources(ctx, pdf, resources, *form)
	if err != nil {
		return err
	}

	wrapper, err := pdf.NewStreamDictForBuf([]byte("q\n" + fit.Matrix.contentMatrix() + "\n/" + wrapperName + " Do\nQ\n"))
	if err != nil {
		return fmt.Errorf("fit wrapper: %w", err)
	}

	ref, err := storeFitStream(ctx, pdf, wrapper)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return fmt.Errorf("fit content: %w", err)
	}

	page[keyResources] = outputResources
	page["Contents"] = *ref
	page[keyMediaBox] = types.NewNumberArray(0, 0, fit.Target.Width, fit.Target.Height)
	page[keyCropBox] = page[keyMediaBox].Clone()
	page[keyRotate] = types.Integer(0)
	page["UserUnit"] = types.Integer(1)

	return nil
}

// Resource-less source Forms and Type3 programs can fall back to the page.
// Retain that complete scope without mutating shared dictionaries or making
// the source Form refer back to its own wrapper binding.
func fittedPageResources(
	ctx context.Context,
	pdf *model.Context,
	resources types.Object,
	form types.IndirectRef,
) (types.Dict, string, error) {
	original, err := pdf.DereferenceDictContext(ctx, resources)
	if err != nil {
		return nil, "", fmt.Errorf("fit original resources: %w", err)
	}

	xobjects, err := pdf.DereferenceDictContext(ctx, original[keyXObject])
	if err != nil {
		return nil, "", fmt.Errorf("fit original XObjects: %w", err)
	}

	name := fitContentName
	for suffix := 1; xobjects[name] != nil; suffix++ {
		name = fitContentName + strconv.Itoa(suffix)
	}

	output, bindings := maps.Clone(original), maps.Clone(xobjects)
	if output == nil {
		output = types.Dict{}
	}

	if bindings == nil {
		bindings = types.Dict{}
	}

	bindings[name] = form
	output[keyXObject] = bindings

	return output, name, nil
}

func fitForm(
	ctx context.Context,
	pdf *model.Context,
	content []byte,
	resources types.Object,
	visible [4]float64,
) (*types.IndirectRef, error) {
	stream, err := pdf.NewStreamDictForBuf(content)
	if err != nil {
		return nil, fmt.Errorf("fit form: %w", err)
	}

	stream.Dict[keyType] = types.Name(keyXObject)
	stream.Dict[keySubtype] = types.Name(formXObjectSubtype)
	stream.Dict["FormType"] = types.Integer(1)
	stream.Dict[keyBBox] = types.NewNumberArray(visible[0], visible[1], visible[2], visible[3])
	stream.Dict["Matrix"] = types.NewNumberArray(1, 0, 0, 1, 0, 0)
	stream.Dict[keyResources] = resources

	return storeFitStream(ctx, pdf, stream)
}

func storeFitStream(ctx context.Context, pdf *model.Context, stream *types.StreamDict) (*types.IndirectRef, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("store fitted stream canceled: %w", ctxErr)
	}

	if err := stream.Encode(); err != nil {
		return nil, fmt.Errorf("encode fitted content: %w", err)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("store fitted stream canceled: %w", ctxErr)
	}

	ref, err := pdf.IndRefForNewObject(*stream)
	if err != nil {
		return nil, fmt.Errorf("store fitted content: %w", err)
	}

	return ref, nil
}
