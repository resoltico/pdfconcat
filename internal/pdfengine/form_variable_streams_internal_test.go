// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"maps"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type (
	variableStyleCase struct {
		properties types.Dict
		name       string
	}
	variableStyleFixture struct {
		pdf         *model.Context
		widget      types.Dict
		phase, font types.IndirectRef
	}
)

const variableSourceFontName = "Actual"

func TestVariableStreamRejectsCancellationBeforeAllocating(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	size := *pdf.Size
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	plan := &variableAppearancePlan{}
	if _, err := plan.stream(ctx, pdf); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}

	if *pdf.Size != size {
		t.Fatal("canceled rendering allocated an object")
	}
}

func TestVariableStreamDiscardsWorkCanceledBeforeAllocation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		budget int64
	}{{name: "drawn", budget: 1}, {name: "encoded", budget: 2}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			size := *pdf.Size
			ctx := newFormCheckpointContext(t.Context(), t)
			ctx.remaining.Store(test.budget)

			plan := &variableAppearancePlan{width: 100, height: 50, fontSize: 12}
			if _, err := plan.stream(ctx, pdf); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation during rendering lost: %v", err)
			}

			if *pdf.Size != size {
				t.Fatal("canceled rendering allocated an object")
			}
		})
	}
}

func TestVariableStreamRejectsFiniteInputOverflow(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		plan variableAppearancePlan
	}{
		{name: "clip", plan: variableAppearancePlan{width: 100, height: 100, border: math.MaxFloat64, fontSize: 12}},
		{name: "advance", plan: variableAppearancePlan{
			width: 100, height: 100, fontSize: 12,
			lines: []variableAppearanceLine{{width: math.MaxFloat64}},
		}},
		{name: "multiline-baseline", plan: variableAppearancePlan{
			width: 100, height: 100, fontSize: math.MaxFloat64,
			lines: []variableAppearanceLine{{}, {}},
		}},
		{name: "comb-center", plan: variableAppearancePlan{
			width: 100, height: 100, fontSize: 12, maxLen: 2,
			glyphs: []variableAppearanceLine{{width: math.MaxFloat64}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)

			size := *pdf.Size
			if _, err := test.plan.stream(t.Context(), pdf); !errors.Is(err, errFormState) {
				t.Fatalf("unsafe computed geometry accepted: %v", err)
			}

			if *pdf.Size != size {
				t.Fatal("rejected geometry allocated an object")
			}
		})
	}
}

func TestVariableNormalStreamOwnsSourceResourceBindings(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	font := signatureTestObject(pdf, types.Dict{
		keyType: types.Name(keyFont), keySubtype: types.Name(type1FontSubtype),
		keyBaseFont: types.Name(fontHelvetica),
	})
	resources := types.Dict{keyFont: types.Dict{variableSourceFontName: font}}
	plan := &variableAppearancePlan{
		width: 100, height: 50, fontSize: 20, da: "/Actual 20 Tf 0 g",
		resources: resources, lines: []variableAppearanceLine{{data: []byte("Source"), width: 3}},
	}

	ref, err := plan.stream(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}

	stream := buttonStream(t, pdf, *ref)
	actual := buttonDictionary(t, pdf, stream.Dict[keyResources])

	bindings := buttonDictionary(t, pdf, actual[keyFont])
	if bindings[variableSourceFontName] != font {
		t.Fatal("normal appearance substituted the actual source font binding")
	}

	if len(stream.Content) == 0 || stream.Dict[keySubtype] != types.Name(formXObjectSubtype) {
		t.Fatal("normal appearance is not a populated form stream")
	}
}

func TestRequestedVariableNormalPreservesSharedAlternateScopes(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	font := signatureTestObject(pdf, types.Dict{
		keySubtype:  types.Name(type1FontSubtype),
		keyBaseFont: types.Name(fontHelvetica),
	})
	phase := signatureTestObject(pdf, types.StreamDict{Dict: types.Dict{
		keyType: types.Name(keyXObject), keySubtype: types.Name(formXObjectSubtype),
		"BBox": types.NewNumberArray(0, 0, 100, 40), keyResources: types.Dict{keyFont: types.Dict{variableSourceFontName: font}},
	}, Content: []byte(emptyAppearanceDrawing)})
	original := types.Dict{"N": phase, "D": phase, "R": phase}
	fields := make(types.Array, 0, 2)
	widgets := make([]types.Dict, 0, 2)

	for index, width := range []float64{100, 150} {
		widget := types.Dict{
			keySubtype: types.Name(widgetSubtype), "FT": types.Name("Tx"),
			"T": types.StringLiteral(string(rune('A' + index))), "V": types.StringLiteral("Actual value"),
			keyRect: types.NewNumberArray(0, 0, width, 40), "AP": original,
		}
		fields = append(fields, signatureTestObject(pdf, widget))
		widgets = append(widgets, widget)
	}

	form := types.Dict{
		keyFields: fields, "DA": types.StringLiteral("/Actual 12 Tf 0 g"), "Q": types.Integer(2),
		"NeedAppearances": types.Boolean(true), "DR": types.Dict{keyFont: types.Dict{variableSourceFontName: font}},
	}

	pdf.RootDict[keyAcroForm] = form
	if _, err := prepareFormResources(t.Context(), pdf, 9); err != nil {
		t.Fatal(err)
	}

	if original["N"] != phase || form["NeedAppearances"] != types.Boolean(false) {
		t.Fatal("shared source appearance changed or regeneration remains global")
	}

	for _, widget := range widgets {
		assertVariableAppearanceScopes(t, pdf, widget, phase, font)
	}

	first := buttonDictionary(t, pdf, widgets[0]["AP"])

	second := buttonDictionary(t, pdf, widgets[1]["AP"])
	if first["N"] == second["N"] {
		t.Fatal("distinct widget geometries share a normal appearance")
	}
}

func assertVariableAppearanceScopes(t *testing.T, pdf *model.Context, widget types.Dict, phase, font types.IndirectRef) {
	t.Helper()

	appearance := buttonDictionary(t, pdf, widget["AP"])
	if appearance["D"] != phase || appearance["R"] != phase {
		t.Fatal("alternate source appearance references changed")
	}

	stream := buttonStream(t, pdf, appearance["N"])
	resources := buttonDictionary(t, pdf, stream.Dict[keyResources])

	fonts := buttonDictionary(t, pdf, resources[keyFont])
	if fonts[variableSourceFontName] != font || widget["Q"] != types.Integer(2) || widget["V"] != types.StringLiteral("Actual value") {
		t.Fatal("normal appearance lost actual font, inherited justification, or field value")
	}
}

func TestRequestedVariableStylesRetainLogicalStateAndIndependentPhases(t *testing.T) {
	t.Parallel()

	for _, test := range variableStyleCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := variableStyleSource(t, test.properties)
			pdf, widget := fixture.pdf, fixture.widget
			value, flags := widget["V"], widget["Ff"]

			if _, err := prepareFormResources(t.Context(), pdf, 3); err != nil {
				t.Fatal(err)
			}

			appearance := buttonDictionary(t, pdf, widget["AP"])
			if appearance["D"] != fixture.phase || widget["V"] != value || widget["Ff"] != flags {
				t.Fatal("normal generation changed logical state or an alternate appearance")
			}

			stream := buttonStream(t, pdf, appearance["N"])
			resources := buttonDictionary(t, pdf, stream.Dict[keyResources])

			fonts := buttonDictionary(t, pdf, resources[keyFont])
			if fonts[variableSourceFontName] != fixture.font || len(stream.Content) == 0 {
				t.Fatal("style lost actual source resources or its generated normal stream")
			}
		})
	}
}

func variableStyleCases() []variableStyleCase {
	return []variableStyleCase{
		{name: "single-left", properties: types.Dict{"Q": types.Integer(0)}},
		{name: "single-center", properties: types.Dict{"Q": types.Integer(1)}},
		{name: "single-right", properties: types.Dict{"Q": types.Integer(2)}},
		{name: "empty-value", properties: types.Dict{"V": types.StringLiteral("")}},
		{
			name:       "multiline",
			properties: types.Dict{"Ff": types.Integer(variableMultilineFlag), "V": types.StringLiteral("First line\nNext line")},
		},
		{
			name:       "comb-left",
			properties: types.Dict{"Ff": types.Integer(variableCombFlag), keyMaxLen: types.Integer(8), "Q": types.Integer(0)},
		},
		{
			name:       "comb-center",
			properties: types.Dict{"Ff": types.Integer(variableCombFlag), keyMaxLen: types.Integer(8), "Q": types.Integer(1)},
		},
		{
			name:       "comb-right",
			properties: types.Dict{"Ff": types.Integer(variableCombFlag), keyMaxLen: types.Integer(8), "Q": types.Integer(2)},
		},
		{
			name: "list",
			properties: types.Dict{
				"FT":  types.Name("Ch"),
				"Opt": types.Array{types.StringLiteral("A"), types.StringLiteral("B")},
				"V":   types.StringLiteral("B"),
			},
		},
		{name: "quarter-turn", properties: types.Dict{"MK": types.Dict{"R": types.Integer(variableQuarterTurn)}}},
		{name: "half-turn", properties: types.Dict{"MK": types.Dict{"R": types.Integer(variableHalfTurn)}}},
		{name: "three-quarter-turn", properties: types.Dict{"MK": types.Dict{"R": types.Integer(variableThreeQuarterTurn)}}},
		{name: "gray-solid", properties: variableBorderProperties("S", types.NewNumberArray(.4))},
		{name: "RGB-dashed", properties: variableBorderProperties("D", types.NewNumberArray(.1, .2, .3))},
		{name: "CMYK-underlined", properties: variableBorderProperties("U", types.NewNumberArray(.1, .2, .3, .4))},
		{name: "RGB-beveled", properties: variableBorderProperties("B", types.NewNumberArray(.2, .3, .4))},
		{name: "CMYK-inset", properties: variableBorderProperties("I", types.NewNumberArray(.1, .2, .3, .4))},
		{
			name:       "background-border",
			properties: types.Dict{"MK": types.Dict{"BG": types.NewNumberArray(.8)}, "BS": types.Dict{"W": types.Integer(1)}},
		},
		{name: "invisible-border", properties: types.Dict{"BS": types.Dict{"W": types.Integer(1)}}},
	}
}

func variableBorderProperties(style string, color types.Array) types.Dict {
	return types.Dict{
		"MK": types.Dict{"BG": color, "BC": color},
		"BS": types.Dict{"S": types.Name(style), "W": types.Float(1.25), "D": types.NewNumberArray(3, 1)},
	}
}

func variableStyleSource(t *testing.T, properties types.Dict) variableStyleFixture {
	t.Helper()
	pdf := signatureTestContext(t)
	font := signatureTestObject(pdf, types.Dict{keySubtype: types.Name(type1FontSubtype), keyBaseFont: types.Name(fontHelvetica)})
	phase := signatureTestObject(pdf, types.StreamDict{Dict: types.Dict{
		keyType: types.Name(keyXObject), keySubtype: types.Name(formXObjectSubtype), "BBox": types.NewNumberArray(0, 0, 100, 40),
	}, Content: []byte(emptyAppearanceDrawing)})
	widget := types.Dict{
		keySubtype: types.Name(widgetSubtype), "FT": types.Name("Tx"), "T": types.StringLiteral("value"),
		"V": types.StringLiteral("ABC"), "Ff": types.Integer(0), keyRect: types.NewNumberArray(0, 0, 100, 40), "AP": types.Dict{"D": phase},
	}
	maps.Copy(widget, properties)
	field := signatureTestObject(pdf, widget)
	pdf.RootDict[keyAcroForm] = types.Dict{
		keyFields: types.Array{field}, "DA": types.StringLiteral("/Actual 12 Tf 0 g"),
		"NeedAppearances": types.Boolean(true), "DR": types.Dict{keyFont: types.Dict{variableSourceFontName: font}},
	}

	return variableStyleFixture{pdf: pdf, widget: widget, phase: phase, font: font}
}
