// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestFitType3FractionalTJSpacingAndRotatedAdvance(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)

	widths := make(types.Array, 34)
	for index := range widths {
		widths[index] = types.Float(125.5)
	}

	font := types.Dict{
		keyType:         types.Name(keyFont),
		keySubtype:      types.Name(guardType3Name),
		guardFontMatrix: types.NewNumberArray(0.002, 0.003, -0.004, 0.005, 0, 0),
		keyEncoding: types.Dict{
			keyDifferences: types.Array{types.Integer(32), types.Name("space"), types.Integer(65), types.Name("A")},
		},
		guardCharProcs: types.Dict{},
		guardFirstChar: types.Integer(32),
		guardLastChar:  types.Integer(65),
		guardWidths:    widths,
	}
	scope := types.Dict{keyFont: types.Dict{"F": font}}
	program := "BT /F 40 Tf 0.25 Tc 0.75 Tw 50 Tz 1 0 0 1 10 20 Tm [(A) 125.25 ( )] TJ"
	state := executeFitTextProgram(t, inspector, scope, program)
	// Horizontal displacement: (125.5*.002*40+.25)*.5 - 125.25/1000*40*.5
	// plus the space displacement (125.5*.002*40+.25+.75)*.5 = 8.16.
	assertFitTextPosition(t, state, 18.16, 20)
}

func TestFitGraphicsRestoreKeepsTextPositionAndRestoresParameters(t *testing.T) {
	t.Parallel()

	pdf := guardContext(t)
	inspector := newFitProgramInspector(pdf)
	font := types.Dict{
		keyType: types.Name(keyFont), keySubtype: types.Name(guardType3Name),
		guardFontMatrix: types.NewNumberArray(0.002, 0.003, -0.004, 0.005, 0, 0),
		keyEncoding:     types.Dict{keyDifferences: types.Array{types.Integer(65), types.Name("A")}},
		guardCharProcs:  types.Dict{}, guardFirstChar: types.Integer(65), guardLastChar: types.Integer(65),
		guardWidths: types.Array{types.Float(125.5)},
	}
	scope := types.Dict{keyFont: types.Dict{"F": font}}
	program := "BT /F 40 Tf 0.25 Tc 50 Tz 1 0 0 1 10 20 Tm q 20 Tc (A) Tj Q (A) Tj"
	state := executeFitTextProgram(t, inspector, scope, program)
	// Q restores Tc=.25 but the first text displacement remains: 15.02 + 5.145.
	assertFitTextPosition(t, state, 30.165, 20)

	if state.charSpace != .25 {
		t.Fatalf("Q did not restore character spacing: %g", state.charSpace)
	}
}

func executeFitTextProgram(t *testing.T, inspector *fitProgramInspector, scope types.Dict, program string) *fitGraphicsState {
	t.Helper()

	instructions, err := inspector.parse(t.Context(), "text-position-control", []byte(program), scope)
	if err != nil {
		t.Fatal(err)
	}

	state := &fitGraphicsState{matrix: fitIdentityMatrix(), sourceMatrix: fitIdentityMatrix(), horizontal: 1}

	var stack []fitGraphicsState
	for _, instruction := range instructions {
		if err = inspector.execute(t.Context(), instruction, scope, state, &stack); err != nil {
			t.Fatal(err)
		}
	}

	return state
}

func assertFitTextPosition(t *testing.T, state *fitGraphicsState, x, y float64) {
	t.Helper()

	matrix := state.textMatrix
	if math.Abs(matrix[4]-x) > 1e-12 || matrix[5] != y {
		t.Fatalf("text position (%g,%g), want (%g,%g)", matrix[4], matrix[5], x, y)
	}
}
