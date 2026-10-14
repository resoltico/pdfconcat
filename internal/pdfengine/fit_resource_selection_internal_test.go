// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitResourceLookupRejectsNilAndConsumesWork(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	scope := types.Dict{keyXObject: types.Dict{"Nil": nil, "Present": types.Integer(1)}}
	if _, err := inspector.resource(t.Context(), scope, keyXObject, "Nil"); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("nil resource accepted: %v", err)
	}
	inspector.work = fitResourceWorkLimit - 1
	if _, err := inspector.resource(t.Context(), scope, keyXObject, "Present"); err != nil {
		t.Fatalf("last permitted lookup refused: %v", err)
	}
	if _, err := inspector.resource(t.Context(), scope, keyXObject, "Present"); !errors.Is(err, errFitUnsupported) {
		t.Fatalf("resource work exceeded the declared limit: %v", err)
	}
}

func TestFitColorSpaceSelectionHandlesEmptyAndScalarResources(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		object  types.Object
		pattern bool
	}{
		{types.Array{}, false},
		{types.Name("DeviceRGB"), false},
		{types.Array{types.Name(fitPattern)}, true},
	} {
		inspector := newFitProgramInspector(guardContext(t))
		scope := types.Dict{fitColorSpace: types.Dict{"Selected": fixture.object}}
		got, err := inspector.patternColorSpace(t.Context(), scope, "Selected")
		if err != nil || got != fixture.pattern {
			t.Fatalf("color space %v selected pattern=%t, want %t: %v", fixture.object, got, fixture.pattern, err)
		}
	}
}

func TestFitPaintingConsultsOnlySelectedPatternModes(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	for _, fixture := range []struct {
		state           *fitGraphicsState
		unused, painted fitPaintMode
	}{
		{&fitGraphicsState{fillPattern: true}, fitPaintStroke, fitPaintFill},
		{&fitGraphicsState{strokePattern: true}, fitPaintFill, fitPaintStroke},
	} {
		if err := inspector.paintPatterns(t.Context(), fixture.state, fixture.unused); err != nil {
			t.Fatalf("mode %d consulted an unused pattern: %v", fixture.unused, err)
		}
		if err := inspector.paintPatterns(t.Context(), fixture.state, fixture.painted); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("painted mode %d without pattern selection accepted: %v", fixture.painted, err)
		}
	}
}

func TestFitTextRenderingModeDomainFromActualContent(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"0", "1", "2", "3", "4", "5", "6", "7"} {
		inspector := newFitProgramInspector(guardContext(t))
		if err := guardInspect(t, inspector, "BT "+mode+" Tr ET", nil); err != nil {
			t.Fatalf("supported text rendering mode %s refused: %v", mode, err)
		}
	}
	for _, mode := range []string{"-1", "8", "0.5"} {
		inspector := newFitProgramInspector(guardContext(t))
		if err := guardInspect(t, inspector, "BT "+mode+" Tr ET", nil); err == nil {
			t.Fatalf("unsupported text rendering mode %s accepted", mode)
		}
	}
}

func TestFitSimpleTextConsultsEachPaintedPatternMode(t *testing.T) {
	t.Parallel()
	for mode, painting := range []struct{ fill, stroke bool }{
		{true, false},
		{false, true},
		{true, true},
		{false, false},
		{true, false},
		{false, true},
		{true, true},
		{false, false},
	} {
		for _, selection := range []struct {
			name, program string
			painted       bool
		}{
			{"fill", "/Pattern cs /Missing scn ", painting.fill},
			{"stroke", "/Pattern CS /Missing SCN ", painting.stroke},
		} {
			t.Run(strconv.Itoa(mode)+"-"+selection.name, func(t *testing.T) {
				pdf := guardContext(t)
				scope := types.Dict{keyFont: types.Dict{"F": declaredGuardFont()}}
				program := selection.program + "BT /F 12 Tf " + strconv.Itoa(mode) + " Tr (A) Tj ET"
				err := guardInspect(t, newFitProgramInspector(pdf), program, scope)
				if selection.painted {
					if !errors.Is(err, errFitUnsupported) {
						t.Fatalf("painted missing pattern accepted: %v", err)
					}
				} else if err != nil {
					t.Fatalf("unpainted missing pattern consulted: %v", err)
				}
			})
		}
	}
}

func TestFitTextArrayKeepsEveryNumericAdjustment(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	instructions, err := inspector.parse(t.Context(), "numeric-adjustments", []byte("[100 .5 200] TJ"), nil)
	if err != nil || len(instructions) != 1 {
		t.Fatalf("numeric TJ parse: %v", err)
	}
	state := &fitGraphicsState{textMatrix: fitIdentityMatrix(), fontSize: 10, horizontal: 2}
	if err := inspector.showTextArray(t.Context(), instructions[0].span, state); err != nil {
		t.Fatal(err)
	}
	if math.Abs(state.textMatrix[4]-(-6.01)) > 1e-12 || state.textMatrix[5] != 0 {
		t.Fatalf("TJ displacement = %v, want -6.01 on x", state.textMatrix)
	}
}

func TestFitType3SpaceAdvanceAndGlyphMatrix(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		code byte
		x    float64
	}{{32, 5.6}, {65, 2.1}} {
		state := &fitGraphicsState{textMatrix: fitIdentityMatrix(), fontSize: 2, horizontal: .5, charSpace: 3, wordSpace: 7}
		if err := fitType3Advance(state, Affine{.001, 0, 0, .001, 0, 0}, 600, test.code); err != nil {
			t.Fatal(err)
		}
		if math.Abs(state.textMatrix[4]-test.x) > 1e-12 {
			t.Fatalf("code %d advance = %v, want %v", test.code, state.textMatrix[4], test.x)
		}
	}
	matrix, err := fitGlyphMatrix(fitIdentityMatrix(), fitIdentityMatrix(), fitIdentityMatrix(), 2, .5, 3)
	if err != nil || matrix != (Affine{1, 0, 0, 2, 0, 3}) {
		t.Fatalf("scaled glyph matrix = %v: %v", matrix, err)
	}
	for _, source := range []bool{false, true} {
		state := &fitGraphicsState{matrix: fitIdentityMatrix(), sourceMatrix: fitIdentityMatrix(), textMatrix: fitIdentityMatrix(), fontSize: 2, horizontal: 1}
		if source {
			state.sourceMatrix[0] = math.MaxFloat64
		} else {
			state.matrix[0] = math.MaxFloat64
		}
		if _, err := fitGlyphPlacement(state, &fitType3Context{matrix: fitIdentityMatrix()}); !errors.Is(err, errFitGeometry) {
			t.Fatalf("overflowing source=%v glyph placement accepted: %v", source, err)
		}
	}
}
