// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitUsedImageMaskMetadataAndInlineColorRestrictions(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	image := types.StreamDict{
		Dict:    types.Dict{keyType: types.Name(keyXObject), keySubtype: types.Name(guardImageSubtype), "ImageMask": types.Integer(1)},
		Content: []byte{0},
	}

	scope := types.Dict{keyXObject: types.Dict{"I": image}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "/I Do", scope); err == nil {
		t.Fatal("nonboolean applied ImageMask accepted")
	}

	glyph := guardStream(t, pdf, "0 0 0 0 10 10 d1 BI /W 1 /H 1 /BPC 8 /CS /G ID x EI", nil)
	font := guardType3(t, pdf, types.Dict{"A": glyph})

	scope = types.Dict{keyFont: types.Dict{"F": font}}
	if err := guardInspect(t, newFitProgramInspector(pdf), guardShowFontA, scope); err == nil {
		t.Fatal("d1 accepted an actually painted ordinary inline image")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	state := fitGraphicsState{}
	err := newFitProgramInspector(pdf).paintImage(ctx, types.Dict{"ImageMask": types.Boolean(true)}, &state)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ImageMask resolver cancellation identity lost: %v", err)
	}
}

func TestFitSimpleFontWithoutDescriptorOrKnownMetricsIsHonest(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	inspector := newFitProgramInspector(pdf)
	if width, err := inspector.simpleMissingWidth(t.Context(), types.Dict{}); err != nil || width != 0 {
		t.Fatalf("absent MissingWidth default: %g %v", width, err)
	}

	font := types.Dict{keySubtype: types.Name(nameType1), keyBaseFont: types.Name("Unregistered")}
	scope := types.Dict{keyFont: types.Dict{"F": font}}

	state := executeFitTextProgram(t, inspector, scope, guardShowFontA)
	if state.textPositionKnown {
		t.Fatal("unregistered simple-font metrics fabricated known displacement")
	}

	font[keyBaseFont] = types.Integer(1)

	if err := guardInspect(t, newFitProgramInspector(pdf), guardShowFontA, scope); err == nil {
		t.Fatal("non-name BaseFont metric identity accepted")
	}
}

func TestFitActuallyPaintedImageDomainRefusesIntroducedAffineLoss(t *testing.T) {
	t.Parallel()

	for _, program := range []string{
		"1000000000000001 0 0 1 0 0 cm /I Do",
		"1000000000000001 0 0 1 0 0 cm BI /W 1 /H 1 /BPC 8 /CS /G ID x EI",
	} {
		t.Run(strconv.Itoa(len(program)), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			inspector := newFitProgramInspector(pdf)

			fit, err := CanvasFit(PageSize{100, 100}, PageSize{612, 1008})
			if err != nil {
				t.Fatal(err)
			}

			image := types.StreamDict{Dict: types.Dict{keySubtype: types.Name(guardImageSubtype)}, Content: []byte{0}}
			resources := types.Dict{keyXObject: types.Dict{"I": image}}
			page := types.Dict{keyContents: types.StreamDict{Dict: types.Dict{}, Content: []byte(program)}}

			err = inspector.inspectPage(t.Context(), page, resources, fit)
			if !errors.Is(err, errFitGeometry) {
				t.Fatalf("actual image domain did not reject introduced affine loss: %v", err)
			}
		})
	}
}
