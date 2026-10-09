// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitPaintedPatternMetadataRefusesAtActualUse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		change func(types.Dict)
		name   string
	}{
		{name: "wrong-pattern-type", change: func(dict types.Dict) { dict[guardPatternType] = types.Integer(3) }},
		{name: "noninteger-pattern-type", change: func(dict types.Dict) { dict[guardPatternType] = types.Name(guardBadMetadata) }},
		{name: "missing-resources", change: func(dict types.Dict) { delete(dict, keyResources) }},
		{name: "invalid-resources", change: func(dict types.Dict) { dict[keyResources] = types.Integer(1) }},
		{name: "zero-step", change: func(dict types.Dict) { dict["XStep"] = types.Integer(0) }},
		{name: "nonfinite-step", change: func(dict types.Dict) { dict["YStep"] = types.Float(math.Inf(1)) }},
		{name: "non-number-step", change: func(dict types.Dict) { dict["XStep"] = types.Name(guardBadMetadata) }},
		{name: "invalid-paint-type", change: func(dict types.Dict) { dict[guardPaintType] = types.Integer(3) }},
		{name: "noninteger-paint-type", change: func(dict types.Dict) { dict[guardPaintType] = types.Name(guardBadMetadata) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			pattern := guardPattern(t, pdf, "", types.Dict{})
			entry, _ := pdf.FindTableEntry(pattern.ObjectNumber.Value(), 0)

			stream, isStream := entry.Object.(types.StreamDict)
			if !isStream {
				t.Fatal("pattern fixture is not stream")
			}

			test.change(stream.Dict)

			scope := types.Dict{fitPattern: types.Dict{"P": pattern}}
			if err := guardInspect(t, newFitProgramInspector(pdf), guardSelectPattern, scope); err != nil {
				t.Fatalf("unused malformed pattern rejected before paint: %v", err)
			}

			if err := guardInspect(t, newFitProgramInspector(pdf), guardPaintPattern, scope); err == nil {
				t.Fatal("painted malformed pattern accepted")
			}
		})
	}
}

func TestFitAppliedSoftMaskMetadataRefusesAtActualUse(t *testing.T) {
	t.Parallel()

	masks := []types.Object{
		types.Name(guardInvalidName),
		types.Integer(1),
		types.Dict{},
		types.Dict{"S": types.Name(guardInvalidName)},
		types.Dict{"S": types.Integer(1)},
		types.Dict{"S": types.Name(fitMaskAlpha)},
		types.Dict{"S": types.Name(fitMaskAlpha), "G": types.Dict{}},
		types.Dict{"S": types.Name(fitMaskAlpha), "G": types.StreamDict{Dict: types.Dict{keySubtype: types.Name(guardImageSubtype)}}},
		types.Dict{"S": types.Name(fitMaskAlpha), "G": types.StreamDict{Dict: types.Dict{keySubtype: types.Name(formXObjectSubtype)}}},
		types.Dict{
			"S": types.Name(guardLuminosityMask),
			"G": types.StreamDict{Dict: types.Dict{keySubtype: types.Name(formXObjectSubtype), fitGroup: types.Dict{}}},
		},
	}
	for n, mask := range masks {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			scope := types.Dict{keyExtGState: types.Dict{"G": types.Dict{guardSoftMaskKey: mask}}}
			if err := guardInspect(t, newFitProgramInspector(pdf), "", scope); err != nil {
				t.Fatalf("unused mask rejected: %v", err)
			}

			if err := guardInspect(t, newFitProgramInspector(pdf), guardApplyGraphicsState, scope); err == nil {
				t.Fatal("applied malformed soft mask accepted")
			}
		})
	}
}

func TestFitAppliedFontStateAndRestrictedTransferBoundaries(t *testing.T) {
	t.Parallel()

	for _, object := range []types.Object{
		types.Integer(1),
		types.Array{},
		types.Array{types.Dict{}, types.Name(guardBadMetadata)},
		types.Array{types.Name(guardBadMetadata), types.Integer(12)},
		types.Array{types.Dict{}, types.Float(math.Inf(1))},
	} {
		t.Run(fmt.Sprintf("%T", object), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			scope := types.Dict{keyExtGState: types.Dict{"G": types.Dict{keyFont: object}}}
			if err := guardInspect(t, newFitProgramInspector(pdf), guardApplyGraphicsState, scope); err == nil {
				t.Fatal("malformed applied gs Font accepted")
			}
		})
	}

	if err := fitRestrictedExtGState(types.Dict{"TR": types.Name("Transfer")}); err == nil {
		t.Fatal("uncolored invocation accepted applied transfer function")
	}
}

func TestFitUsedResourceResolversPreserveCancellation(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	state := fitGraphicsState{}
	_, err := inspector.softMaskGroup(ctx, types.Dict{"G": types.StreamDict{Dict: types.Dict{}}}, fitMaskAlpha)

	checks := []error{
		err,
		inspector.softMask(ctx, types.Name("None"), &state, "cancelled"),
		inspector.tilingSteps(ctx, types.Dict{"XStep": types.Integer(1), "YStep": types.Integer(1)}),
		inspector.extGStateFont(ctx, types.Array{types.Dict{}, types.Integer(12)}, &state),
	}

	for _, err := range checks {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("used resolver replaced cancellation identity: %v", err)
		}
	}
}

func TestFitSoftMaskNoneClearsStateAndBoundsUsedBackgroundTransfer(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)

	state := fitGraphicsState{mask: "previous"}
	if err := inspector.softMask(t.Context(), types.Name("None"), &state, "none"); err != nil || state.mask != "" {
		t.Fatalf("SMask None did not clear active state: %s %v", state.mask, err)
	}

	form := guardStream(t, pdf, "", types.Dict{})
	entry, _ := pdf.FindTableEntry(form.ObjectNumber.Value(), 0)

	stream, isStream := entry.Object.(types.StreamDict)
	if !isStream {
		t.Fatal("mask fixture is not a stream")
	}

	stream.Dict[fitGroup] = types.Dict{"S": types.Name(fitTransparency)}

	for _, key := range []string{"BC", "TR"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			used := types.Dict{
				guardSoftMaskKey: types.Dict{"S": types.Name(fitMaskAlpha), "G": form, key: types.Array{types.Float(math.Inf(1))}},
			}

			scope := types.Dict{keyExtGState: types.Dict{"G": used}}
			if err := guardInspect(t, newFitProgramInspector(pdf), guardApplyGraphicsState, scope); err == nil {
				t.Fatal("applied soft-mask background/transfer admitted nonfinite used resource")
			}
		})
	}
}

func TestFitResourceMetadataResolversRetainNativeFailures(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	scope := types.Dict{keyExtGState: types.Dict{"G": types.Dict{}}}

	state := fitGraphicsState{}
	if err := inspector.extGState(ctx, scope, "G", &state); !errors.Is(err, context.Canceled) {
		t.Fatalf("gs resource lookup erased cancellation: %v", err)
	}

	if err := inspector.applyExtGState(ctx, types.Dict{}, &state, "cancel"); !errors.Is(err, context.Canceled) {
		t.Fatalf("gs dictionary resolver erased cancellation: %v", err)
	}

	selection := fitPatternSelection{scope: types.Dict{fitPattern: types.Dict{"P": types.Dict{}}}, name: "P"}
	if _, err := inspector.patternProgram(ctx, selection); !errors.Is(err, context.Canceled) {
		t.Fatalf("pattern resource lookup erased cancellation: %v", err)
	}

	if err := inspector.shadingObject(ctx, types.Dict{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("shading lookup erased cancellation: %v", err)
	}
}

func TestFitPaintedPatternAndShadingObjectTypeFailuresRemainLocated(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	bad := types.Dict{fitPattern: types.Dict{"P": types.Integer(1)}}
	if err := guardInspect(t, newFitProgramInspector(pdf), guardPaintPattern, bad); err == nil {
		t.Fatal("painted nondictionary pattern accepted")
	}

	pattern := types.Dict{
		"PatternType": types.Integer(2),
		fitShading:    types.Integer(1),
		keyExtGState:  types.Dict{keyFont: types.Integer(1)},
	}

	scope := types.Dict{fitPattern: types.Dict{"P": pattern}}
	if err := guardInspect(t, newFitProgramInspector(pdf), guardPaintPattern, scope); err == nil {
		t.Fatal("painted shading pattern ignored malformed applied state")
	}

	pattern[fitMatrix] = types.NewIntegerArray(1, 0, 0)

	if err := guardInspect(t, newFitProgramInspector(pdf), guardPaintPattern, scope); err == nil {
		t.Fatal("painted pattern ignored malformed local Matrix")
	}

	for _, object := range []types.Object{
		types.Integer(1),
		types.StreamDict{Dict: types.Dict{fitColorSpace: types.Name(fitDeviceRGB)}, Content: []byte("opaque mesh data")},
	} {
		scope = types.Dict{fitShading: types.Dict{"S": object}}
		err := guardInspect(t, newFitProgramInspector(pdf), guardPaintShading, scope)

		_, isStream := object.(types.StreamDict)
		if (err == nil) != isStream {
			t.Fatalf("used shading dictionary/stream distinction: %T %v", object, err)
		}
	}
}

func TestFitD1ActuallyAppliedTransferAndNativeColorResolverCancellation(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	graphics := types.Dict{"G": types.Dict{"TR": types.Name("Identity")}}
	glyph := guardStream(t, pdf, "0 0 0 0 10 10 d1 /G gs", nil)
	font := guardType3(t, pdf, types.Dict{"A": glyph})

	scope := types.Dict{keyFont: types.Dict{"F": font}, keyExtGState: graphics}

	transferErr := guardInspect(t, newFitProgramInspector(pdf), guardShowFontA, scope)
	if transferErr == nil || !strings.Contains(transferErr.Error(), "ExtGState /TR") {
		t.Fatalf("strict d1 applied transfer diagnostic lost: %v", transferErr)
	}

	inspector := newFitProgramInspector(pdf)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := inspector.colorComponents(ctx, nil, types.Name(fitDeviceRGB), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("color metrics replaced caller cancellation: %v", err)
	}

	if err := inspector.boundedResource(ctx, types.Dict{}, 0, map[string]bool{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("used function graph replaced caller cancellation: %v", err)
	}

	if err := inspector.groupFlags(ctx, types.Dict{"I": types.Boolean(false)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("group flag resolver replaced caller cancellation: %v", err)
	}

	if err := inspector.groupType(ctx, types.Name(fitGroup)); !errors.Is(err, context.Canceled) {
		t.Fatalf("group type resolver replaced caller cancellation: %v", err)
	}
}

func TestFitActuallyPaintedTilingBoxAndSimpleTextPatternRefuseUnsafeResource(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	scope := types.Dict{}
	pattern := guardPattern(t, pdf, guardPaintPattern, scope)
	scope[fitPattern] = types.Dict{"P": pattern}

	scope[keyFont] = types.Dict{"F": declaredGuardFont()}
	if err := guardInspect(t, newFitProgramInspector(pdf), guardSelectPattern+" BT /F 12 Tf (A) Tj", scope); err == nil {
		t.Fatal("actually filled text ignored recursive selected pattern")
	}

	entry, _ := pdf.FindTableEntry(pattern.ObjectNumber.Value(), 0)

	stream, isStream := entry.Object.(types.StreamDict)
	if !isStream {
		t.Fatal("pattern fixture is not stream")
	}

	stream.Dict[keyBBox] = types.NewIntegerArray(0, 0, 0, 1)

	if err := guardInspect(t, newFitProgramInspector(pdf), guardPaintPattern, scope); err == nil {
		t.Fatal("actually painted tiling pattern accepted empty cell bounds")
	}
}

func TestFitResourceDispatchCancellationAtEveryActualCheckpoint(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	leaf := guardStream(t, pdf, "", nil)
	glyph := guardStream(t, pdf, guardZeroGlyphMetrics, nil)
	font := guardType3(t, pdf, types.Dict{"A": glyph})
	cellResources := types.Dict{keyFont: types.Dict{"F": font}, keyXObject: types.Dict{guardLeafName: leaf}}
	pattern := guardPattern(t, pdf, "BT /F 12 Tf (A) Tj ET /Leaf Do", cellResources)
	mask := guardStream(t, pdf, "", nil)
	entry, _ := pdf.FindTableEntry(mask.ObjectNumber.Value(), 0)

	maskStream, ok := entry.Object.(types.StreamDict)
	if !ok {
		t.Fatal("mask fixture is not stream")
	}

	group := types.Dict{
		keyType: types.Name(fitGroup),
		"S":     types.Name(fitTransparency),
		"I":     types.Boolean(true),
		"K":     types.Boolean(false),
		"CS":    types.Name(fitDeviceRGB),
	}
	maskStream.Dict[fitGroup] = group
	maskSpec := types.Dict{"S": types.Name(fitMaskAlpha), "G": mask}
	scope := types.Dict{
		keyFont:       types.Dict{"F": font},
		fitPattern:    types.Dict{"P": pattern},
		fitColorSpace: types.Dict{"PCS": types.Array{types.Name(fitPattern), types.Name(fitDeviceRGB)}},
		keyExtGState: types.Dict{
			"G": types.Dict{
				keyFont:          types.Array{font, types.Integer(12)},
				guardSoftMaskKey: maskSpec,
			},
		},
	}
	page := types.Dict{
		fitGroup:    group,
		keyContents: types.StreamDict{Dict: types.Dict{}, Content: []byte("/G gs /PCS cs 0 0 0 /P scn 0 0 1 1 re f BT /F 12 Tf (A) Tj ET")},
	}
	fit := PageFit{
		Matrix:   fitIdentityMatrix(),
		Visible:  [4]float64{0, 0, 100, 100},
		Original: PageSize{100, 100},
		Target:   PageSize{100, 100},
		Scale:    1,
		UserUnit: 1,
	}

	for _, kind := range []string{fitMaskAlpha, guardLuminosityMask} {
		maskSpec["S"] = types.Name(kind)

		assertFitResourceCancellationCheckpoints(t, func(ctx context.Context) error {
			return newFitProgramInspector(pdf).inspectPage(ctx, page, scope, fit)
		})
	}
}

func assertFitResourceCancellationCheckpoints(t *testing.T, inspect func(context.Context) error) {
	t.Helper()

	probe := newFormCheckpointContext(t.Context(), t)
	probe.remaining.Store(math.MaxInt64)

	if err := inspect(probe); err != nil {
		t.Fatalf("uncancelled active resource graph refused: %v", err)
	}

	checkpoints := math.MaxInt64 - probe.remaining.Load()
	for budget := range checkpoints + 1 {
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(budget)

		err := inspect(ctx)
		if budget == checkpoints {
			if err != nil {
				t.Fatalf("resource graph did not finish with measured budget %d: %v", budget, err)
			}
		} else if !errors.Is(err, context.Canceled) {
			t.Fatalf("resource checkpoint %d changed cancellation identity: %v", budget, err)
		}
	}
}

func TestFitActualColorSelectionAndMissingShadingBoundaries(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)

	scope := types.Dict{fitColorSpace: types.Dict{guardColorAlias: types.Name(fitDeviceRGB)}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "/Alias cs 0 0 0 sc 0 0 1 1 re f", scope); err != nil {
		t.Fatalf("canonical scalar device-color alias refused: %v", err)
	}

	badScope := types.Dict{fitColorSpace: types.Dict{guardColorAlias: types.Array{types.Integer(1)}}}
	if err := guardInspect(t, newFitProgramInspector(pdf), "/Alias cs", badScope); err == nil {
		t.Fatal("used color-space array accepted non-name family")
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), guardPaintShading, nil); err == nil {
		t.Fatal("painted missing shading accepted")
	}

	inspector := newFitProgramInspector(pdf)

	_, err := inspector.colorComponents(
		t.Context(),
		types.Dict{fitColorSpace: types.Integer(1)},
		types.Name(fitDeviceRGB),
		0,
	)
	if err == nil {
		t.Fatal("inline device-color callback accepted malformed default-color dictionary")
	}

	if _, err = inspector.arrayColorComponents(t.Context(), types.Array{types.Integer(1)}); err == nil {
		t.Fatal("inline color callback accepted non-name family")
	}
}

func TestFitActualEscapedNULNamesRemainRefusedAtEveryRawBoundary(t *testing.T) {
	t.Parallel()

	programs := []string{
		"/Bad#00 Do",
		"/Bad#00 gs",
		"BT /Bad#00 12 Tf",
		"/Bad#00 cs",
		"/Bad#00 CS",
		"/Pattern cs /Bad#00 scn",
		"/Pattern CS /Bad#00 SCN",
		"BI /W 1 /H 1 /BPC 8 /CS /Bad#00 ID \x00 EI",
	}
	for n, program := range programs {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			err := guardInspect(t, newFitProgramInspector(pdf), program, nil)
			if err == nil || !strings.Contains(err.Error(), "null byte") {
				t.Fatalf("raw NUL escape did not reach semantic name refusal for %q: %v", program, err)
			}
		})
	}
}
