// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"errors"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestButtonCompilationCapturesMetricDerivedGeometry(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	dict := buttonTestDictionary()
	dict["BS"] = types.Dict{"S": types.Name("S"), "W": types.Float(1.2)}
	plan, err := compileButtonAppearance(t.Context(), pdf, dict)
	if err != nil {
		t.Fatal(err)
	}
	// The pinned ZapfDingbats AFM gives caption 4 an advance of 846/1000.
	// A 20-point square with 1.2-point border admits size 16, centered at 3.23,3.60.
	if plan.width != 20 || plan.height != 20 || plan.border != 1.2 || plan.fontSize != 16 || plan.textX != 3.23 || plan.textY != 3.6 {
		t.Fatalf("source geometry or caption placement changed: %+v", plan)
	}
	if plan.background != [3]float64{.8, .843, 1} || plan.borderColor != [3]float64{.1, .1, .1} || plan.caption != '4' || plan.on != buttonOnState {
		t.Fatalf("source characteristics or selection changed: %+v", plan)
	}
	for _, test := range []struct{ side, border, size float64 }{{60, 1.2, 57}, {20, 1.3, 15}} {
		dict := buttonTestDictionary()
		dict[keyRect] = types.NewNumberArray(0, 0, test.side, test.side)
		buttonDictionary(t, pdf, dict["BS"])["W"] = types.Float(test.border)
		plan, err := compileButtonAppearance(t.Context(), pdf, dict)
		if err != nil || plan.fontSize != test.size {
			t.Fatalf("height/width constrained caption size: %+v %v", plan, err)
		}
	}
	buttonDictionary(t, pdf, dict["MK"])["BG"] = types.NewNumberArray(0, 0, 0)
	buttonDictionary(t, pdf, dict["MK"])["BC"] = types.NewNumberArray(1, 1, 1)
	if _, err := compileButtonAppearance(t.Context(), pdf, dict); err != nil {
		t.Fatalf("inclusive black/white color boundaries refused: %v", err)
	}
}

func TestButtonSourceOperandDecodeFailuresKeepTheirCause(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"rectangle", "rectangle-number", "BS", "border-style", "border-width", "rotation", "background", "border-color", "background-number", "border-color-number", "AS", "AP", "normal-states"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			ref := failedVariableObject(&formFixture{pdf: pdf})
			dict := buttonTestDictionary()
			switch target {
			case "rectangle":
				dict[keyRect] = ref
			case "rectangle-number":
				dict[keyRect] = types.Array{ref, types.Integer(600), types.Integer(68), types.Integer(620)}
			case "BS":
				dict["BS"] = ref
			case "border-style":
				buttonDictionary(t, pdf, dict["BS"])["S"] = ref
			case "border-width":
				buttonDictionary(t, pdf, dict["BS"])["W"] = ref
			case "rotation":
				buttonDictionary(t, pdf, dict["MK"])["R"] = ref
			case "background":
				buttonDictionary(t, pdf, dict["MK"])["BG"] = ref
			case "border-color":
				buttonDictionary(t, pdf, dict["MK"])["BC"] = ref
			case "background-number":
				buttonDictionary(t, pdf, dict["MK"])["BG"] = types.Array{ref, types.Integer(0), types.Integer(0)}
			case "border-color-number":
				buttonDictionary(t, pdf, dict["MK"])["BC"] = types.Array{ref, types.Integer(0), types.Integer(0)}
			case "normal-states":
				buttonDictionary(t, pdf, dict["AP"])["N"] = ref
			default:
				dict[target] = ref
			}
			if _, err := compileButtonAppearance(t.Context(), pdf, dict); !errors.Is(err, errBoom) || !errors.Is(err, errFormState) {
				t.Fatalf("source %s failure lost its cause or refusal: %v", target, err)
			}
		})
	}
}

func TestButtonSourceMissingBorderAndZeroSizeAreRefused(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"missing-style", "missing-border", "zero-size", "zero-font-size", "negative-color", "infinite-color", "NaN-background", "NaN-border", "short-background", "missing-AS", "null-chained-flags"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if failure := recover(); failure != nil {
					t.Errorf("malformed button source panicked: %v", failure)
				}
			}()
			pdf := signatureTestContext(t)
			dict := buttonTestDictionary()
			switch target {
			case "missing-style":
				buttonDictionary(t, pdf, dict["BS"]).Delete("S")
			case "missing-border":
				dict.Delete("BS")
			case "zero-size":
				dict[keyRect] = types.NewNumberArray(0, 0, 0, 0)
			case "zero-font-size":
				dict[keyRect] = types.NewNumberArray(0, 0, 4.5, 4.5)
				buttonDictionary(t, pdf, dict["BS"])["W"] = types.Float(.25)
			case "negative-color":
				buttonDictionary(t, pdf, dict["MK"])["BC"] = types.NewNumberArray(-.1, 0, 0)
			case "NaN-background":
				buttonDictionary(t, pdf, dict["MK"])["BG"] = types.NewNumberArray(math.NaN(), 0, 0)
			case "NaN-border":
				buttonDictionary(t, pdf, dict["MK"])["BC"] = types.NewNumberArray(0, math.NaN(), 0)
			case "short-background":
				buttonDictionary(t, pdf, dict["MK"])["BG"] = types.NewNumberArray(.5, .5)
			case "missing-AS":
				dict.Delete("AS")
			case "null-chained-flags":
				null := signatureTestObject(pdf, nil)
				dict["Ff"] = signatureTestObject(pdf, null)

			case "infinite-color":
				buttonDictionary(t, pdf, dict["MK"])["BG"] = types.NewNumberArray(math.Inf(1), 0, 0)
			}
			if _, err := compileButtonAppearance(t.Context(), pdf, dict); !errors.Is(err, errFormState) {
				t.Fatalf("source %s accepted: %v", target, err)
			}
		})
	}
}

func TestVariableSourceDimensionsRotationsAndBoundaryColors(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	plan := variableAppearancePlan{}
	if err := plan.readVariableGeometry(t.Context(), pdf, types.Dict{keyRect: types.NewNumberArray(10, 20, 30, 50)}); err != nil || plan.width != 20 || plan.height != 30 {
		t.Fatalf("translated source rectangle dimensions changed: %+v %v", plan, err)
	}
	for _, rotation := range []int{-450, -90, 0, 90, 360, 450} {
		want := map[int]int{-450: 270, -90: 270, 0: 0, 90: 90, 360: 0, 450: 90}[rotation]
		plan := variableAppearancePlan{}
		if err := plan.readVariableCharacteristics(t.Context(), pdf, types.Dict{"MK": types.Dict{"R": types.Integer(rotation), "BG": types.NewNumberArray(0, 1, .5)}}); err != nil || plan.rotation != want || len(plan.background) != 3 || plan.background[0] != 0 || plan.background[1] != 1 {
			t.Fatalf("rotation %d or inclusive color boundaries changed: %+v %v", rotation, plan, err)
		}
	}
	for _, rect := range []types.Array{types.NewNumberArray(0, 0, 20, 0), types.NewNumberArray(0, -1e308, 1, 1e308)} {
		if err := plan.readVariableGeometry(t.Context(), pdf, types.Dict{keyRect: rect}); !errors.Is(err, errFormState) {
			t.Fatalf("invalid source height accepted: %v", err)
		}
	}
	for _, color := range []types.Array{types.NewNumberArray(-.1), types.NewNumberArray(1.1), {types.Name("invalid")}, types.NewNumberArray(math.Inf(1)), types.NewNumberArray(math.NaN())} {
		if _, err := readVariableColor(t.Context(), pdf, color); !errors.Is(err, errFormState) {
			t.Fatalf("invalid source color accepted: %v", err)
		}
	}
	dash, err := readVariableDash(t.Context(), pdf, types.NewNumberArray(0, 3))
	if err != nil || len(dash) != 2 || dash[0] != 0 || dash[1] != 3 {
		t.Fatalf("zero dash component with positive sibling refused: %v %v", dash, err)
	}
	for _, dash := range []types.Array{types.NewNumberArray(-1, 3), types.NewNumberArray(math.NaN(), 3), {types.Name("invalid"), types.Integer(3)}} {
		if _, err := readVariableDash(t.Context(), pdf, dash); !errors.Is(err, errFormState) {
			t.Fatalf("invalid dash accepted: %v", err)
		}
	}
}

func TestButtonOffSelectionStillRequiresValidStateDictionaryAndValueType(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	off := buttonTestDictionary()
	off["AS"], off["V"] = types.Name(buttonOffState), types.Name(buttonOffState)
	if _, err := compileButtonAppearance(t.Context(), pdf, off); err != nil {
		t.Fatalf("valid off-selected checkbox refused: %v", err)
	}
	for _, target := range []string{"extra-state", "missing-off", "empty-on", "scalar-radio-value", "unknown-checkbox-value"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			dict := buttonTestDictionary()
			dict["AS"], dict["V"] = types.Name(buttonOffState), types.Name(buttonOffState)
			stream := types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}
			switch target {
			case "extra-state":
				buttonDictionary(t, pdf, dict["AP"])["N"] = types.Dict{buttonOffState: stream, "First": stream, "Second": stream}
			case "missing-off":
				buttonDictionary(t, pdf, dict["AP"])["N"] = types.Dict{"First": stream, "Second": stream}
			case "empty-on":
				buttonDictionary(t, pdf, dict["AP"])["N"] = types.Dict{buttonOffState: stream, "": stream}
			case "unknown-checkbox-value":
				dict["V"] = types.Name("Other")
			case "scalar-radio-value":
				dict["Ff"], dict["V"] = types.Integer(checkboxRadioFlag), types.Integer(1)
				buttonDictionary(t, pdf, dict["MK"])["CA"] = types.StringLiteral("l")
			}
			if _, err := compileButtonAppearance(t.Context(), pdf, dict); !errors.Is(err, errFormState) {
				t.Fatalf("invalid off-selected source %s accepted: %v", target, err)
			}
		})
	}
}

func TestButtonInheritedSourceAllowsLastDepthAndRefusesNextParent(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	leaf := types.Dict{"Ff": types.Integer(2)}
	root := leaf
	for range maxFieldDepth {
		root = types.Dict{keyParent: root}
	}
	value, err := buttonInherited(t.Context(), pdf, root, "Ff")
	if err != nil || value.value != types.Integer(2) {
		t.Fatalf("last permitted inherited field refused: %v %v", value, err)
	}
	if _, err := buttonInherited(t.Context(), pdf, types.Dict{keyParent: root}, "Ff"); !errors.Is(err, errFormState) {
		t.Fatalf("one excess inherited parent accepted: %v", err)
	}
}

func TestVariableSourceMalformedArrayAndNullRotationReturnRefusal(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"long-rectangle", "null-rotation", "three-entry-border"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if failure := recover(); failure != nil {
					t.Errorf("source refusal panicked: %v", failure)
				}
			}()
			pdf := signatureTestContext(t)
			plan := variableAppearancePlan{}
			var err error
			switch target {
			case "long-rectangle":
				err = plan.readVariableGeometry(t.Context(), pdf, types.Dict{keyRect: types.NewNumberArray(0, 0, 20, 30, 40)})
			case "null-rotation":
				err = plan.readVariableCharacteristics(t.Context(), pdf, types.Dict{"MK": types.Dict{"R": nil}})
			case "three-entry-border":
				err = plan.readVariableBorder(t.Context(), pdf, types.Dict{"Border": types.NewNumberArray(0, 0, 1.25)})
				if err != nil || plan.border != 1.25 || plan.borderStyle != "S" {
					t.Fatalf("valid three-entry border changed: %+v %v", plan, err)
				}
				return
			}
			if !errors.Is(err, errFormState) {
				t.Fatalf("malformed source %s accepted: %v", target, err)
			}
		})
	}
}

func TestButtonGeometryRefusesZeroAndNonfiniteDimensionsBeforeMetrics(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	for _, rect := range []types.Array{types.NewNumberArray(0, 0, 0, 0), types.NewNumberArray(-1e308, -1e308, 1e308, 1e308), types.NewNumberArray(0, -1e308, 20, 1e308), types.NewNumberArray(0, 0, 20, 30)} {
		dict := buttonTestDictionary()
		dict[keyRect] = rect
		plan := buttonAppearancePlan{}
		if err := plan.readGeometry(t.Context(), pdf, dict); !errors.Is(err, errFormState) {
			t.Fatalf("invalid dimensions reached metric preparation: %v", err)
		}
	}
}

func TestButtonRoundedCoordinatesMustEachRemainFinite(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		side  float64
		radio bool
	}{{side: 1.9e307}, {side: 1.73e307, radio: true}} {
		pdf := signatureTestContext(t)
		dict := buttonTestDictionary()
		dict[keyRect] = types.NewNumberArray(0, 0, test.side, test.side)
		if test.radio {
			dict["Ff"] = types.Integer(checkboxRadioFlag)
			buttonDictionary(t, pdf, dict["MK"])["CA"] = types.StringLiteral("l")
		}
		if _, err := compileButtonAppearance(t.Context(), pdf, dict); !errors.Is(err, errFormState) {
			t.Fatalf("one overflowing rounded coordinate accepted (radio=%t): %v", test.radio, err)
		}
	}
}
