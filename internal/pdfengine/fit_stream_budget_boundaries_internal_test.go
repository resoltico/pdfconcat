// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/filter"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitLogicalProgramBudgetAndStreamCountExactBoundaries(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	stream := types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}
	for _, limit := range []int64{0, -1, fitProgramByteLimit + 1} {
		if _, err := fitLogicalContentLimit(t.Context(), pdf, types.Dict{keyContents: stream}, limit); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("invalid program budget accepted: %d %v", limit, err)
		}
	}

	content, err := fitLogicalContentLimit(t.Context(), pdf, types.Dict{keyContents: stream}, 3)
	if err != nil || string(content) != emptyAppearanceDrawing {
		t.Fatalf("exact single stream bytes: %q %v", content, err)
	}

	content, err = fitLogicalContentLimit(t.Context(), pdf, types.Dict{keyContents: types.Array{nil, stream, nil}}, 4)
	if err != nil || string(content) != "q Q\n" {
		t.Fatalf("absent stream array member altered boundaries: %q %v", content, err)
	}

	many := make(types.Array, fitContentStreamLimit+1)
	_, err = fitLogicalContentLimit(t.Context(), pdf, types.Dict{keyContents: many}, fitProgramByteLimit)

	if !errors.Is(err, errFitUnsupported) {
		t.Fatalf("stream count+1 accepted: %v", err)
	}

	many = many[:fitContentStreamLimit]
	if _, err = fitLogicalContentLimit(t.Context(), pdf, types.Dict{keyContents: many}, fitProgramByteLimit); err != nil {
		t.Fatalf("exact absent stream count rejected: %v", err)
	}
}

func TestFitProgramDecoderBoundsRawCachedAndMalformedFilterInput(t *testing.T) {
	t.Parallel()

	raw := types.StreamDict{Dict: types.Dict{}, Raw: make([]byte, fitProgramByteLimit+1)}
	if _, err := fitDecodeProgram(t.Context(), &raw, fitProgramByteLimit); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("encoded program+1 accepted: %v", err)
	}

	cached := types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}
	if _, err := fitDecodeProgram(t.Context(), &cached, 2); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("cached decoded program+1 accepted: %v", err)
	}

	encoded := types.StreamDict{Dict: types.Dict{}, Raw: []byte(emptyAppearanceDrawing)}
	if _, err := fitDecodeProgram(t.Context(), &encoded, 2); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("unfiltered raw program+1 accepted: %v", err)
	}

	malformed := types.StreamDict{
		Dict:           types.Dict{},
		Raw:            []byte("invalid compressed bytes"),
		FilterPipeline: []types.PDFFilter{{Name: filter.Flate}},
	}
	if _, err := fitDecodeProgram(t.Context(), &malformed, 128); err == nil {
		t.Fatal("malformed Flate program accepted")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := fitDecodeProgram(ctx, &cached, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached program cancelled identity lost: %v", err)
	}
}

func TestFitLogicalStreamConcatenationChargesOnlyActualDecodedBytes(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	stream := types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}
	_, err := fitLogicalContentLimit(t.Context(), pdf, types.Dict{keyContents: types.Array{stream, stream}}, 4)

	if !errors.Is(err, errFitUnsupported) {
		t.Fatalf("second material stream bypassed exhausted aggregate budget: %v", err)
	}

	for _, limit := range []int64{0, fitProgramByteLimit + 1} {
		if _, budgetErr := fitDecodeProgram(t.Context(), &stream, limit); !errors.Is(budgetErr, errFitUnsupported) {
			t.Fatalf("invalid direct decoder budget accepted: %d %v", limit, budgetErr)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, cancelErr := fitJoinContentStreams(ctx, pdf, types.Array{stream}, nil, 4); !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("stream loop lost caller cancellation: %v", cancelErr)
	}
}
