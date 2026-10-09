// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func declaredGuardFont() types.Dict {
	return types.Dict{
		keyType: types.Name(keyFont), keySubtype: types.Name("TrueType"), guardFirstChar: types.Integer(65),
		guardWidths: types.Array{types.Float(650.5)}, "FontDescriptor": types.Dict{guardMissingWidth: types.Float(400.25)},
	}
}

func TestFitDeclaredSimpleFontWidthsAndMissingWidthPreservePosition(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	scope := types.Dict{keyFont: types.Dict{"F": declaredGuardFont()}}
	state := executeFitTextProgram(t, newFitProgramInspector(pdf), scope,
		"BT /F 20 Tf .25 Tc .75 Tw 50 Tz 1 0 0 1 10 20 Tm (AB ) Tj")
	// The authored byte widths are .6505, .40025, .40025 em, independently of Unicode glyph names.
	assertFitTextPosition(t, state, 25.26, 20)
}

func TestFitDeclaredSimpleFontMalformedWidthMetadataRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		change func(types.Dict)
		name   string
	}{
		{name: "negative-first-char", change: func(font types.Dict) { font[guardFirstChar] = types.Integer(-1) }},
		{name: "overflowing-first-char", change: func(font types.Dict) { font[guardFirstChar] = types.Integer(256) }},
		{name: "noninteger-first-char", change: func(font types.Dict) { font[guardFirstChar] = types.Name(guardBadMetadata) }},
		{name: "non-array-widths", change: func(font types.Dict) { font[guardWidths] = types.Name(guardBadMetadata) }},
		{name: "range-past-last-code", change: func(font types.Dict) {
			font[guardFirstChar] = types.Integer(255)
			font[guardWidths] = types.NewIntegerArray(1, 2)
		}},
		{name: "non-number-width", change: func(font types.Dict) { font[guardWidths] = types.Array{types.Name(guardBadMetadata)} }},
		{name: "nonfinite-width", change: func(font types.Dict) { font[guardWidths] = types.Array{types.Float(math.Inf(1))} }},
		{name: "non-dictionary-descriptor", change: func(font types.Dict) { font["FontDescriptor"] = types.Name(guardBadMetadata) }},
		{
			name: "non-number-missing-width",
			change: func(font types.Dict) {
				font["FontDescriptor"] = types.Dict{guardMissingWidth: types.Name(guardBadMetadata)}
			},
		},
		{
			name: "nonfinite-missing-width",
			change: func(font types.Dict) {
				font["FontDescriptor"] = types.Dict{guardMissingWidth: types.Float(math.Inf(1))}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			font := declaredGuardFont()
			test.change(font)

			scope := types.Dict{keyFont: types.Dict{"F": font}}
			if err := guardInspect(t, newFitProgramInspector(pdf), "BT /F 20 Tf (A) Tj", scope); err == nil {
				t.Fatal("malformed source byte width metadata accepted")
			}
		})
	}
}

func TestFitUnknownCompositeAdvanceStaysUnknownAcrossGraphicsRestore(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	glyph := guardStream(t, pdf, guardZeroGlyphMetrics, nil)
	type3 := guardType3(t, pdf, types.Dict{"A": glyph})
	composite := types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name("Type0")}
	scope := types.Dict{keyFont: types.Dict{guardUnknownFont: composite, "Known": type3}}

	program := "BT /Known 12 Tf 1 0 0 1 10 20 Tm q /Unknown 12 Tf (A) Tj Q (A) Tj"
	if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); err == nil {
		t.Fatal("Q fabricated known position after unresolved composite advance")
	}
}

func TestFitSimpleFontMetadataPreservesNativeCancellation(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := inspector.simpleMissingWidth(ctx, declaredGuardFont()); !errors.Is(err, context.Canceled) {
		t.Fatalf("MissingWidth cancelled resolver identity: %v", err)
	}

	if _, err := inspector.declaredSimpleMetrics(ctx, declaredGuardFont()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Widths cancelled resolver identity: %v", err)
	}
}

func TestFitSourceEncodingResolversPreserveCancellationAndLazyMalformedMembers(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	encoding := simpleencodings.Encoding{}
	if _, err := inspector.simpleEncoding(ctx, types.Name("WinAnsiEncoding"), &encoding); !errors.Is(err, context.Canceled) {
		t.Fatalf("encoding resolver erased cancellation: %v", err)
	}

	font := types.Dict{
		keySubtype: types.Name(nameType1), keyBaseFont: types.Name(fontHelvetica),
		keyEncoding: types.Dict{keyDifferences: types.Array{types.Integer(65), malformedLazyObject(pdf)}},
	}

	scope := types.Dict{keyFont: types.Dict{"F": font}}
	if err := guardInspect(t, newFitProgramInspector(pdf), guardShowFontA, scope); err == nil {
		t.Fatal("malformed lazy glyph-name member approved byte metrics")
	}
}

func TestFitSimpleFontDisplacementOverflowRefusesOriginalFiniteMetadata(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	font := declaredGuardFont()
	font[guardWidths] = types.Array{types.Float(1e308)}

	scope := types.Dict{keyFont: types.Dict{"F": font}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "BT /F 14000 Tf (A) Tj", scope); !errors.Is(err, errFitGeometry) {
		t.Fatalf("finite authored width/font size produced unsafe text advance: %v", err)
	}
}
