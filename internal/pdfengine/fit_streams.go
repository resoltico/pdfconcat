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

const (
	keyContents                     = "Contents"
	fitProgramByteLimit       int64 = 16 << 20
	fitSourceProgramByteLimit int64 = 128 << 20
	fitContentStreamLimit           = 16384
)

// fitLogicalContentLimit preserves stream boundaries and bounds decoding before allocation.
// Native PageContent joins arrays without separators and has a larger default cap.
func fitLogicalContentLimit(ctx context.Context, pdf *model.Context, page types.Dict, limit int64) ([]byte, error) {
	if backend := pdf.Limits.MaxDecodeBytes; backend > 0 {
		limit = min(limit, backend)
	}

	if limit <= 0 || limit > fitProgramByteLimit {
		return nil, fmt.Errorf("%w: invalid remaining logical program byte budget", errFitUnsupported)
	}

	object, err := pdf.DereferenceContext(ctx, page[keyContents])
	if err != nil {
		return nil, fmt.Errorf("fit content reference: %w", err)
	}

	if object == nil {
		return nil, model.ErrNoContent
	}

	streams, array := object.(types.Array)
	if !array {
		streams = types.Array{object}
	}

	if len(streams) > fitContentStreamLimit {
		return nil, fmt.Errorf("%w: content stream count exceeds %d", errFitUnsupported, fitContentStreamLimit)
	}

	var separator []byte
	if array {
		separator = []byte{'\n'}
	}

	return fitJoinContentStreams(ctx, pdf, streams, separator, limit)
}

func fitJoinContentStreams(ctx context.Context, pdf *model.Context, streams types.Array, separator []byte, limit int64) ([]byte, error) {
	var content []byte

	for _, object := range streams {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fit content streams: %w", err)
		}

		stream, readErr := fitReadProgramStream(ctx, pdf, object)
		if errors.Is(readErr, model.ErrNoContent) {
			continue
		}

		if readErr != nil {
			return nil, fmt.Errorf("fit content stream: %w", readErr)
		}

		remaining := limit - int64(len(content)) - int64(len(separator))

		if remaining <= 0 {
			return nil, fmt.Errorf("%w: logical content exceeds decoded byte limit %d", errFitUnsupported, limit)
		}

		decoded, decodeErr := fitDecodeProgram(ctx, stream, remaining)
		if decodeErr != nil {
			return nil, fitResourceError(decodeErr)
		}

		content = append(content, decoded...)
		content = append(content, separator...)
	}

	return content, nil
}

func fitDecodeProgram(ctx context.Context, stream *types.StreamDict, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fitResourceError(err)
	}

	if limit <= 0 || limit > fitProgramByteLimit {
		return nil, fmt.Errorf("%w: decoded program budget exhausted", errFitUnsupported)
	}

	if int64(len(stream.Raw)) > fitProgramByteLimit {
		return nil, fmt.Errorf("%w: encoded program exceeds byte limit %d", errFitUnsupported, fitProgramByteLimit)
	}

	if int64(len(stream.Content)) > limit || stream.FilterPipeline == nil && int64(len(stream.Raw)) > limit {
		return nil, fmt.Errorf("%w: program exceeds decoded byte limit %d", errFitUnsupported, limit)
	}

	if err := stream.DecodeWithContextAndLimit(ctx, limit); err != nil {
		return nil, fmt.Errorf("fit bounded content decode: %w", err)
	}

	// The native bounded API owns the decoded-size postcondition, including cached Content.

	if err := ctx.Err(); err != nil {
		return nil, fitResourceError(err)
	}

	return stream.Content, nil
}

// fitReadProgramStream resolves a stream without DereferenceStreamDict marking it validator-valid.
// Preflight must not cause the later semantic validator to skip an originally unvalidated object.
func fitReadProgramStream(ctx context.Context, pdf *model.Context, object types.Object) (*types.StreamDict, error) {
	resolved, err := pdf.DereferenceContext(ctx, object)
	if err != nil {
		return nil, fmt.Errorf("fit program stream reference: %w", err)
	}

	if resolved == nil {
		return nil, fmt.Errorf("fit stream is absent: %w", model.ErrNoContent)
	}

	stream, ok := resolved.(types.StreamDict)
	if !ok {
		return nil, fmt.Errorf("%w: invoked content is not a stream (%T)", errFitUnsupported, resolved)
	}

	return &stream, nil
}
