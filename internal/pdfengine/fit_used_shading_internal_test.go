// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

func fitAxialShading() types.Dict {
	return types.Dict{
		"ShadingType": types.Integer(2), fitColorSpace: types.Name(fitDeviceRGB),
		"Coords": types.NewIntegerArray(0, 0, 100, 0), "Extend": types.Array{types.Boolean(true), types.Boolean(true)},
		"Function": types.Dict{
			"FunctionType": types.Integer(2), "Domain": types.NewIntegerArray(0, 1),
			"C0": types.NewIntegerArray(1, 0, 0), "C1": types.NewIntegerArray(0, 0, 1), "N": types.Integer(1),
		},
	}
}

func TestFitUsedShadingAndFunctionGraphBounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mutation func(types.Dict)
		name     string
		reject   bool
	}{
		{name: "valid-function", mutation: func(types.Dict) {}},
		{name: "unrelated-owner-cycle", mutation: func(dict types.Dict) { dict["Owner"] = dict }},
		{name: "used-function-cycle", mutation: func(dict types.Dict) { dict["Function"] = dict }, reject: true},
		{
			name:     "used-nonfinite-range",
			mutation: func(dict types.Dict) { dict["Range"] = types.Array{types.Float(0), types.Float(math.Inf(1))} },
			reject:   true,
		},
		{name: "used-function-stream", mutation: func(dict types.Dict) {
			dict["Function"] = types.StreamDict{
				Dict:    types.Dict{"Domain": types.NewIntegerArray(0, 1)},
				Content: []byte("opaque sampled function"),
			}
		}},
		{
			name:     "unsupported-color-space",
			mutation: func(dict types.Dict) { dict[fitColorSpace] = types.Array{types.Name(fitPattern)} },
			reject:   true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			shading := fitAxialShading()
			test.mutation(shading)

			scope := types.Dict{fitShading: types.Dict{"S": shading}}
			if err := guardInspect(t, newFitProgramInspector(pdf), guardPaintShading, scope); (err != nil) != test.reject {
				t.Fatalf("used shading graph reject=%t: %v", test.reject, err)
			}
		})
	}
}

func guardShadingDocument(t *testing.T, program string) *model.Context {
	t.Helper()

	pdf := guardContext(t)
	page := guardPage(t, pdf)
	shading := fitAxialShading()
	scope := types.Dict{fitShading: types.Dict{"S": shading}}
	scope[fitPattern] = types.Dict{"P": types.Dict{
		"PatternType": types.Integer(2), fitShading: shading,
		keyExtGState: types.Dict{"CA": types.Float(1), "ca": types.Float(1)},
	}}

	page[keyMediaBox] = types.NewIntegerArray(0, 0, 100, 100)
	page[keyCropBox] = types.NewIntegerArray(0, 0, 100, 100)
	page[keyResources] = scope
	page[keyContents] = guardStream(t, pdf, program, nil)

	return pdf
}

func TestFitShadingPatternsPreserveIndependentColorLandmarks(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)
	for _, target := range []PageSize{{210 * 72 / 25.4, 297 * 72 / 25.4}, {612, 1008}} {
		t.Run(targetName(target), func(t *testing.T) { t.Parallel(); checkShadingRender(t, tools, target) })
	}
}

func checkShadingRender(t *testing.T, tools pdforacle.Tools, target PageSize) {
	t.Helper()

	dir := t.TempDir()
	source := guardShadingDocument(t, guardPaintShading)
	fitted := guardShadingDocument(t, "/Pattern cs /P scn 0 0 100 100 re f")
	sourcePath := filepath.Join(dir, fitRenderSource)
	fittedPath := filepath.Join(dir, fitRenderOutput)

	guardWriteDocument(t, source, sourcePath)

	if _, err := inspectPageFits(t.Context(), fitted, target); err != nil {
		t.Fatal(err)
	}

	fitStaticTestPages(t, fitted, target)
	guardWriteDocument(t, fitted, fittedPath)
	original := guardRender(t, tools, sourcePath, fitRendererPoppler)
	output := guardRender(t, tools, fittedPath, fitRendererPoppler)

	for _, x := range []int{10, 30, 50, 70, 90} {
		want := guardRGB(original, x, 50)

		got := guardRGB(output, int((float64(x)+.5)*target.Width/100), int(target.Height/2))
		for channel := range want {
			if difference := int(want[channel]) - int(got[channel]); difference < -4 || difference > 4 {
				t.Fatalf("shading landmark x=%d: source=%v fitted=%v", x, want, got)
			}
		}
	}
}
