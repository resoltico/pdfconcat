// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func TestFitStreamArrayPreservesRenderedTokenBoundary(t *testing.T) {
	t.Parallel()
	tools := pdforacle.RequireTools(t)
	pdf := readUnvalidated(t, pdffixture.FitMarkStreams())
	fitStaticTestPages(t, pdf, PageSize{612, 1008})
	path := filepath.Join(t.TempDir(), "stream-array.pdf")

	built := pool{pdf: pdf}
	if _, err := built.write(t.Context(), path); err != nil {
		t.Fatal(err)
	}

	document, err := pdforacle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}

	verifyFitSheet(t, document, fitPaperLegal)

	colors, err := document.ColorCounts(1, [][3]uint8{{255, 0, 0}, {0, 0, 255}})
	if err != nil || colors[0] != 0 || colors[1] < 10000 {
		t.Fatalf("stream-array clipping/colors %v: %v", colors, err)
	}
}

func TestFitLogicalContentLimitsErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, pdffixture.Plain("logical stream"))
	stream := types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}
	page := types.Dict{keyContents: types.Array{stream, stream}}

	content, err := fitLogicalContentLimit(t.Context(), pdf, page, fitProgramByteLimit)
	if err != nil || string(content) != "q Q\nq Q\n" {
		t.Fatalf("logical boundary: %q %v", content, err)
	}

	pdf.Limits.MaxDecodeBytes = 7
	if _, err = fitLogicalContentLimit(t.Context(), pdf, page, fitProgramByteLimit); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("logical byte cap ignored: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err = fitLogicalContentLimit(ctx, pdf, page, fitProgramByteLimit); !errors.Is(err, context.Canceled) {
		t.Fatalf("logical cancellation ignored: %v", err)
	}

	page[keyContents] = types.Array{types.Integer(7)}
	if _, err = fitLogicalContentLimit(t.Context(), pdf, page, fitProgramByteLimit); err == nil {
		t.Fatal("malformed stream accepted")
	}

	page[keyContents] = malformedLazyObject(pdf)
	if _, err = fitLogicalContentLimit(t.Context(), pdf, page, fitProgramByteLimit); err == nil {
		t.Fatal("malformed stream reference accepted")
	}

	page[keyContents] = nil
	if _, err = fitLogicalContentLimit(t.Context(), pdf, page, fitProgramByteLimit); !errors.Is(err, model.ErrNoContent) {
		t.Fatalf("empty-content semantics: %v", err)
	}
}
