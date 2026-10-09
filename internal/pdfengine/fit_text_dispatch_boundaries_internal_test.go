// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitTextLineAndQuotedOperatorsPreserveHorizontalPosition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, program string
		x, y          float64
	}{
		{name: "next-line", program: "10 TL T* (A) Tj", x: 23.01, y: 10},
		{name: "single-quote", program: "10 TL (A) '", x: 23.01, y: 10},
		{name: "double-quote", program: "10 TL 2 3 (A) \"", x: 26.01, y: 10},
		{name: "move-and-leading", program: "20 -5 TD T* (A) Tj", x: 43.01, y: 10},
		{name: "move-line", program: "20 -5 Td (A) Tj", x: 43.01, y: 15},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			scope := types.Dict{keyFont: types.Dict{"F": declaredGuardFont()}}
			state := executeFitTextProgram(t, newFitProgramInspector(pdf), scope,
				"BT /F 20 Tf 12 Ts 1 Tr 1 0 0 1 10 20 Tm "+test.program)
			assertFitTextPosition(t, state, test.x, test.y)

			if state.rise != 12 || state.textRender != 1 {
				t.Fatal("text parameters were discarded")
			}
		})
	}
}

func TestFitSourceByteEncodingDifferencesAndFallbacksRefuseMalformedMetadata(t *testing.T) {
	t.Parallel()

	tooMany := make(types.Array, fitEncodingDifferenceLimit+1)
	for n := range tooMany {
		tooMany[n] = types.Integer(65)
	}

	cases := []types.Object{
		types.Integer(1), types.Name("unsupported"),
		types.Dict{keyBaseEncoding: types.Name("unsupported")},
		types.Dict{keyBaseEncoding: types.Integer(1)},
		types.Dict{keyDifferences: types.Name("not-array")},
		types.Dict{keyDifferences: tooMany},
		types.Dict{keyDifferences: types.Array{types.Name("A")}},
		types.Dict{keyDifferences: types.Array{types.Integer(-1)}},
		types.Dict{keyDifferences: types.Array{types.Integer(256)}},
		types.Dict{keyDifferences: types.Array{types.Integer(255), types.Name("A"), types.Name("B")}},
		types.Dict{keyDifferences: types.Array{types.Integer(65), types.Float(1)}},
		types.Dict{keyDifferences: types.Array{types.Integer(65), types.StringLiteral("bad#zz")}},
	}
	for n, encoding := range cases {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			font := types.Dict{keySubtype: types.Name(nameType1), keyBaseFont: types.Name(fontHelvetica), keyEncoding: encoding}

			scope := types.Dict{keyFont: types.Dict{"F": font}}
			if err := guardInspect(t, newFitProgramInspector(pdf), guardShowFontA, scope); err == nil {
				t.Fatal("malformed byte-code encoding approved text displacement")
			}
		})
	}
}

func TestFitSourceDeclaredEncodingChangesActualAdvance(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	font := types.Dict{
		keySubtype:  types.Name(nameType1),
		keyBaseFont: types.Name(fontHelvetica),
		keyEncoding: types.Dict{
			keyBaseEncoding: types.Name("WinAnsiEncoding"),
			keyDifferences:  types.Array{types.Integer(65), types.Name("space")},
		},
	}
	scope := types.Dict{keyFont: types.Dict{"F": font}}
	state := executeFitTextProgram(t, newFitProgramInspector(pdf), scope, "BT /F 10 Tf 1 0 0 1 10 20 Tm (A) Tj")
	// Helvetica space is 278/1000 em; source code A maps to space, not the Unicode letter A.
	assertFitTextPosition(t, state, 12.78, 20)
}

func TestFitPaintOperatorsExecuteOnlyTheirSelectedPatternChannels(t *testing.T) {
	t.Parallel()

	for _, operator := range []string{"S", "s", "B", "B*", "b", "b*"} {
		t.Run(operator, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			scope := types.Dict{}
			pattern := guardPattern(t, pdf, guardPaintPattern, scope)
			scope[fitPattern] = types.Dict{"P": pattern}

			program := "/Pattern CS /P SCN 0 0 1 1 re " + operator
			if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); err == nil {
				t.Fatal("stroke-capable paint ignored an actively recursive selected stroke pattern")
			}
		})
	}
}

func TestFitInvisibleSimpleTextDoesNotExecuteSelectedPaintPattern(t *testing.T) {
	t.Parallel()

	for _, mode := range []int{3, 7} {
		t.Run(strconv.Itoa(mode), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			scope := types.Dict{keyFont: types.Dict{"F": declaredGuardFont()}}
			pattern := guardPattern(t, pdf, guardPaintPattern, scope)
			scope[fitPattern] = types.Dict{"P": pattern}

			program := guardSelectPattern + " BT /F 12 Tf " + strconv.Itoa(mode) + " Tr (A) Tj"
			if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); err != nil {
				t.Fatalf("nonpainted text executed a selected pattern: %v", err)
			}
		})
	}
}

func TestFitEmptyTextNeedsNoInventedFontButPaintedTextDoes(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	if err := guardInspect(t, newFitProgramInspector(pdf), "BT () Tj [] TJ ET", nil); err != nil {
		t.Fatalf("empty text fabricated incoming font requirement: %v", err)
	}

	if err := guardInspect(t, newFitProgramInspector(pdf), "BT (A) Tj ET", nil); err == nil {
		t.Fatal("painted nonempty text without incoming font accepted")
	}
}

func TestFitOrdinaryStrokeColorClearsPreviouslySelectedPattern(t *testing.T) {
	t.Parallel()

	for _, color := range []string{"0 G", "0 0 0 RG", "0 0 0 1 K"} {
		t.Run(color, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			scope := types.Dict{}
			pattern := guardPattern(t, pdf, guardPaintPattern, scope)
			scope[fitPattern] = types.Dict{"P": pattern}

			program := "/Pattern CS /P SCN " + color + " 0 0 1 1 re S"
			if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); err != nil {
				t.Fatalf("ordinary stroke color retained stale pattern: %v", err)
			}
		})
	}
}

func TestFitColorSpaceAndRenderingModeBoundaryRefusals(t *testing.T) {
	t.Parallel()

	for _, program := range []string{"/Bad#zz cs", "/Bad#zz Do", "BT /F 12 Tf 8 Tr (A) Tj"} {
		t.Run(program, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			scope := types.Dict{keyFont: types.Dict{"F": declaredGuardFont()}}
			if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); err == nil {
				t.Fatal("malformed source color/resource name or text render mode accepted")
			}
		})
	}

	pdf := guardContext(t)

	scope := types.Dict{fitColorSpace: types.Dict{"RGB": types.Array{types.Name(fitCalRGB), types.Dict{}}}}
	for _, program := range []string{"/DeviceRGB cs 0 0 0 rg", "/DeviceCMYK CS 0 0 0 1 K", "/RGB CS 0 0 0 SCN"} {
		if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); err != nil {
			t.Fatalf("ordinary color space was treated as pattern: %v", err)
		}
	}
}

func TestFitActualTextMovementAndTJFiniteOperandsRejectArithmeticOverflow(t *testing.T) {
	t.Parallel()

	huge := "1" + strings.Repeat("0", 308) + ".0"

	cases := []string{
		"BT 1 0 0 1 " + huge + " 0 Tm " + huge + " 0 Td",
		"BT /F 14000 Tf [" + huge + "] TJ",
		"BT /F 14000 Tf 14000 TL 1 0 0 " + huge + " 0 0 Tm () '",
		"BT /F 14000 Tf 14000 TL 1 0 0 " + huge + " 0 0 Tm 0 0 () \"",
	}
	for n, program := range cases {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)

			scope := types.Dict{keyFont: types.Dict{"F": declaredGuardFont()}}
			if err := guardInspect(t, newFitProgramInspector(pdf), program, scope); !errors.Is(err, errFitGeometry) {
				t.Fatalf("actual text arithmetic overflow must have geometry identity: %v", err)
			}
		})
	}
}

func TestFitPaintRequiresSelectedPatternAndArrayTextRequiresIncomingFont(t *testing.T) {
	t.Parallel()

	for _, program := range []string{"/Pattern cs 0 0 1 1 re f", "/Pattern CS 0 0 1 1 re S", "BT [(A)] TJ"} {
		t.Run(program, func(t *testing.T) {
			t.Parallel()

			pdf := guardContext(t)
			if err := guardInspect(t, newFitProgramInspector(pdf), program, nil); err == nil {
				t.Fatal("painted content without required pattern/font selection accepted")
			}
		})
	}
}
