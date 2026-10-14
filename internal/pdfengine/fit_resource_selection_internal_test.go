// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/benoitkugler/pdf/fonts/simpleencodings"
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

func TestFitDeclaredSimpleMetricsCoverFirstAndLastByteCodes(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	for _, first := range []int{0, 255} {
		metrics, err := inspector.declaredSimpleMetrics(t.Context(), types.Dict{
			"FirstChar": types.Integer(first), "Widths": types.NewNumberArray(1500),
			"FontDescriptor": types.Dict{"MissingWidth": types.Integer(500)},
		})
		if err != nil || !metrics.known || metrics.widths[first] != 1.5 || metrics.widths[128] != .5 {
			t.Fatalf("declared first code %d metrics = %+v: %v", first, metrics, err)
		}
	}
	widths := make(types.Array, 256)
	for index := range widths {
		widths[index] = types.Integer(1000)
	}
	metrics, err := inspector.declaredSimpleMetrics(t.Context(), types.Dict{"FirstChar": types.Integer(0), "Widths": widths})
	if err != nil || metrics.widths[0] != 1 || metrics.widths[255] != 1 {
		t.Fatalf("inclusive complete byte width range refused: %v", err)
	}
	for _, first := range []int{-1, 256} {
		if _, err := inspector.declaredSimpleMetrics(t.Context(), types.Dict{"FirstChar": types.Integer(first), "Widths": types.NewNumberArray(1000)}); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("out-of-range first code %d accepted: %v", first, err)
		}
	}
}

func TestFitEncodingAndType3WidthsCoverInclusiveByteRange(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	for _, code := range []byte{0, 255} {
		encoding := simpleencodings.Encoding{}
		position, err := fitEncodingEntry(types.Integer(code), -1, &encoding)
		if err != nil || position != int(code) {
			t.Fatalf("encoding position %d refused: %v", code, err)
		}
		position, err = fitEncodingEntry(types.Name("A"), position, &encoding)
		if err != nil || position != int(code)+1 || encoding[code] != "A" {
			t.Fatalf("encoding assignment %d changed: %v", code, err)
		}
		width, err := inspector.type3Width(t.Context(), types.Dict{
			"FirstChar": types.Integer(code), "LastChar": types.Integer(code), "Widths": types.NewNumberArray(600),
		}, code)
		if err != nil || width != 600 {
			t.Fatalf("Type3 singleton code %d width = %v: %v", code, width, err)
		}
	}
	for _, bounds := range [][2]int{{-1, 0}, {255, 256}, {65, 64}} {
		widths := types.Array{}
		code := byte(65)
		switch bounds[0] {
		case -1:
			widths, code = types.NewNumberArray(300, 600), 0
		case 255:
			widths, code = types.NewNumberArray(300, 600), 255
		}
		if _, err := inspector.type3Width(t.Context(), types.Dict{
			"FirstChar": types.Integer(bounds[0]), "LastChar": types.Integer(bounds[1]), "Widths": widths,
		}, code); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("invalid Type3 character range %v accepted: %v", bounds, err)
		}
	}
	for _, length := range []int{1024, 1025} {
		values := make(types.Array, length)
		for index := range values {
			values[index] = types.Integer(0)
		}
		encoding := simpleencodings.Encoding{}
		_, err := inspector.fitEncodingDifferences(t.Context(), types.Dict{keyDifferences: values}, &encoding)
		if length == 1024 && err != nil || length == 1025 && !errors.Is(err, errFitUnsupported) {
			t.Fatalf("Differences entry count %d: %v", length, err)
		}
	}
}

func TestFitResourceBoxRejectsEachNonpositiveAxis(t *testing.T) {
	t.Parallel()
	inspector := newFitProgramInspector(guardContext(t))
	inspector.fit = PageFit{Visible: [4]float64{0, 0, 100, 100}, Matrix: fitIdentityMatrix()}
	state := &fitGraphicsState{matrix: fitIdentityMatrix(), sourceMatrix: fitIdentityMatrix()}
	for _, box := range [][4]float64{{0, 0, 0, 1}, {0, 0, 1, 0}} {
		dict := types.Dict{"BBox": types.NewNumberArray(box[0], box[1], box[2], box[3])}
		if err := inspector.resourceBox(t.Context(), dict, "BBox", state); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("empty invoked resource box %v accepted: %v", box, err)
		}
	}
	for _, box := range [][4]float64{{1, 0, 0, 1}, {0, 1, 1, 0}} {
		if err := inspector.programBox(box, box, state); !errors.Is(err, errFitUnsupported) {
			t.Fatalf("reversed declared box %v accepted: %v", box, err)
		}
	}
}

func TestCanvasFitRejectsNonpositiveAndNonfiniteDimensions(t *testing.T) {
	t.Parallel()
	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		for _, canvas := range []PageSize{{Width: value, Height: 100}, {Width: 100, Height: value}} {
			if _, err := CanvasFit(canvas, PageSize{Width: 100, Height: 100}); !errors.Is(err, errFitGeometry) {
				t.Fatalf("invalid authored canvas %+v accepted: %v", canvas, err)
			}
		}
		for _, target := range []PageSize{{Width: value, Height: 100}, {Width: 100, Height: value}} {
			if _, err := CanvasFit(PageSize{Width: 100, Height: 100}, target); !errors.Is(err, errFitGeometry) {
				t.Fatalf("invalid fit target %+v accepted: %v", target, err)
			}
		}
	}
}
