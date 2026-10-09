// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	emptyAppearanceDrawing = "q Q"
	variableAutoDA         = "/F1 0 Tf"
)

func variableTestFixture() *formFixture {
	fixture := formTestContext()
	fixture.form["NeedAppearances"] = types.Boolean(true)
	fixture.parent["V"] = types.StringLiteral("VALID VARIABLE")
	fixture.widget["Rect"] = types.NewNumberArray(0, 0, 280, 40)
	fixture.widget["AP"] = types.Dict{"D": types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}}

	return fixture
}

func TestVariableSourceCompilationRejectsMalformedGeometryAndDefaults(t *testing.T) {
	t.Parallel()

	cases := malformedVariableShapes()
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := variableTestFixture()
			corrupt(fixture)

			if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
				t.Fatalf("malformed variable shape accepted: %v", err)
			}
		})
	}
}

func malformedVariableShapes() map[string]func(*formFixture) {
	return map[string]func(*formFixture){
		"scalar rectangle": func(f *formFixture) { f.widget["Rect"] = types.Integer(1) },
		"short rectangle":  func(f *formFixture) { f.widget["Rect"] = types.NewNumberArray(0, 0) },
		"invalid coordinate": func(f *formFixture) {
			f.widget["Rect"] = types.Array{types.Name("x"), types.Integer(0), types.Integer(20), types.Integer(20)}
		},
		"infinite coordinate":    func(f *formFixture) { f.widget["Rect"] = types.NewNumberArray(0, 0, math.Inf(1), 40) },
		"overflow dimensions":    func(f *formFixture) { f.widget["Rect"] = types.NewNumberArray(-1e308, 0, 1e308, 40) },
		"zero width":             func(f *formFixture) { f.widget["Rect"] = types.NewNumberArray(0, 0, 0, 40) },
		"scalar characteristics": func(f *formFixture) { f.widget["MK"] = types.Integer(1) },
		"rotation type":          func(f *formFixture) { f.widget["MK"] = types.Dict{"R": types.Name("90")} },
		"rotation angle":         func(f *formFixture) { f.widget["MK"] = types.Dict{"R": types.Integer(10)} },
		"color type":             func(f *formFixture) { f.widget["MK"] = types.Dict{"BG": types.Integer(1)} },
		"color arity":            func(f *formFixture) { f.widget["MK"] = types.Dict{"BG": types.NewNumberArray(0, 0)} },
		"color range":            func(f *formFixture) { f.widget["MK"] = types.Dict{"BC": types.NewNumberArray(2)} },
		"scalar BS":              func(f *formFixture) { f.widget["BS"] = types.Integer(1) },
		"border width type":      func(f *formFixture) { f.widget["BS"] = types.Dict{"W": types.Name("WidthMustBeNumeric")} },
		"negative border":        func(f *formFixture) { f.widget["BS"] = types.Dict{"W": types.Integer(-1)} },
		"border style type":      func(f *formFixture) { f.widget["BS"] = types.Dict{"S": types.Integer(1)} },
		"unknown border style":   func(f *formFixture) { f.widget["BS"] = types.Dict{"S": types.Name("UnrecognizedBorderStyle")} },
		"scalar Border":          func(f *formFixture) { f.widget["Border"] = types.Integer(1) },
		"Border length":          func(f *formFixture) { f.widget["Border"] = types.NewNumberArray(0, 0) },
		"Border width type": func(f *formFixture) {
			f.widget["Border"] = types.Array{types.Integer(0), types.Integer(0), types.Name("WidthMustBeNumeric")}
		},
		"scalar dash":   func(f *formFixture) { f.widget["BS"] = types.Dict{"S": types.Name("D"), "D": types.Integer(1)} },
		"negative dash": func(f *formFixture) { f.widget["BS"] = types.Dict{"S": types.Name("D"), "D": types.NewNumberArray(-1)} },
		"zero dash": func(f *formFixture) {
			f.widget["BS"] = types.Dict{"S": types.Name("D"), "D": types.NewNumberArray(0, 0)}
		},
		"scalar flags":    func(f *formFixture) { f.parent["Ff"] = types.Name("flags") },
		"negative MaxLen": func(f *formFixture) { f.parent["MaxLen"] = types.Integer(-1) },
		"nontext value":   func(f *formFixture) { f.parent["V"] = types.Dict{} },
		"multiple text values": func(f *formFixture) {
			f.parent["V"] = types.Array{types.StringLiteral("ONE"), types.StringLiteral("TWO")}
		},
		"comb missing MaxLen": func(f *formFixture) { f.parent["Ff"] = types.Integer(variableCombFlag) },
		"comb multiline": func(f *formFixture) {
			f.parent["Ff"] = types.Integer(variableCombFlag | variableMultilineFlag)
			f.parent["MaxLen"] = types.Integer(8)
		},
		"negative font size": func(f *formFixture) { f.parent["DA"] = types.StringLiteral("/F1 -1 Tf") },
		"tiny autosize rectangle": func(f *formFixture) {
			f.parent["DA"] = types.StringLiteral(variableAutoDA)
			f.widget["Rect"] = types.NewNumberArray(0, 0, 1, 1)
		},
	}
}

func TestVariableCompilationPreservesInheritedStateAndMaskedValues(t *testing.T) {
	t.Parallel()

	fixture := variableTestFixture()
	fixture.parent["Ff"] = types.Integer(variablePasswordFlag)
	fixture.parent["V"] = types.HexLiteral("feff004100420043")

	state, err := analyzeForm(t.Context(), fixture.pdf)
	if err != nil {
		t.Fatal(err)
	}

	var captured *variableAppearancePlan

	for _, entry := range state.defaults {
		if entry.variable != nil {
			captured = entry.variable
		}
	}

	if captured == nil || len(captured.lines) != 1 || string(captured.lines[0].data) != "***" {
		t.Fatalf("password not masked: %+v", captured)
	}

	if captured.justification != 2 || captured.fontSize != 12 {
		t.Fatalf("inherited defaults changed: %+v", captured)
	}

	if fixture.parent["V"] != types.HexLiteral("feff004100420043") {
		t.Fatal("logical password value changed")
	}
}

func TestVariableCompilationCancellationRetainsSourceGraph(t *testing.T) {
	t.Parallel()

	const checkpoints = 2048

	complete := false

	for budget := range checkpoints {
		fixture := variableTestFixture()
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(int64(budget))

		_, err := analyzeForm(ctx, fixture.pdf)
		if err == nil {
			complete = true
			break
		}

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("variable cancellation checkpoint%d: %v", budget, err)
		}

		if _, changed := fixture.widget.Find("DA"); changed {
			t.Fatal("compilation changed source defaults")
		}
	}

	if !complete {
		t.Fatal("finite variable cancellation controls never completed")
	}
}
