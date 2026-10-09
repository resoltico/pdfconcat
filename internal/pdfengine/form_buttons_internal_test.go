// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const buttonOnState = "Yes"

func buttonTestDictionary() types.Dict {
	stream := types.StreamDict{Dict: types.Dict{}, Content: []byte(emptyAppearanceDrawing)}

	return types.Dict{
		keyRect: types.NewNumberArray(48, 600, 68, 620),
		"BS":    types.Dict{"S": types.Name("S"), "W": types.Integer(1)},
		"MK": types.Dict{
			"BG": types.NewNumberArray(.8, .843, 1),
			"BC": types.NewNumberArray(.1, .1, .1),
			"CA": types.StringLiteral("4"),
		},
		"Ff": types.Integer(2),
		"V":  types.Name(buttonOnState),
		"AS": types.Name(buttonOnState),
		"AP": types.Dict{
			"N": types.Dict{"Off": stream, buttonOnState: stream},
			"D": types.Name("unchanged-down"),
			"R": types.Name("unchanged-rollover"),
		},
	}
}

func TestButtonMaterializationUsesValidOwnedAppearanceStreams(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	dict := buttonTestDictionary()

	plan, err := compileButtonAppearance(t.Context(), pdf, dict)
	if err != nil {
		t.Fatal(err)
	}

	if err = plan.apply(t.Context(), pdf, dict); err != nil {
		t.Fatal(err)
	}

	appearance := buttonDictionary(t, pdf, dict["AP"])
	normal := buttonDictionary(t, pdf, appearance["N"])

	for _, state := range []string{"Off", buttonOnState} {
		stream := buttonStream(t, pdf, normal[state])
		if len(stream.Content) == 0 || stream.NameEntry(keySubtype) == nil || *stream.NameEntry(keySubtype) != "Form" {
			t.Fatal("invalid normal appearance")
		}

		if (state == buttonOnState) != strings.Contains(string(stream.Content), "Tj") {
			t.Fatalf("wrong state content %s", stream.Content)
		}
	}

	if dict["V"] != types.Name(buttonOnState) || dict["AS"] != types.Name(buttonOnState) ||
		appearance["D"] != types.Name("unchanged-down") {
		t.Fatal("logical/down state changed")
	}
}

func TestButtonMaterializationDoesNotOverwriteSharedAppearanceScopes(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	first, second := buttonTestDictionary(), buttonTestDictionary()
	shared := buttonDictionary(t, pdf, first["AP"])
	second["AP"] = shared

	second[keyRect] = types.NewNumberArray(48, 600, 64, 616)
	for _, dict := range []types.Dict{first, second} {
		plan, err := compileButtonAppearance(t.Context(), pdf, dict)
		if err != nil {
			t.Fatal(err)
		}

		if err = plan.apply(t.Context(), pdf, dict); err != nil {
			t.Fatal(err)
		}
	}

	for index, dict := range []types.Dict{first, second} {
		appearance := buttonDictionary(t, pdf, dict["AP"])
		normal := buttonDictionary(t, pdf, appearance["N"])
		stream := buttonStream(t, pdf, normal[buttonOnState])

		bbox, err := pdf.DereferenceArray(stream.Dict["BBox"])
		if err != nil || len(bbox) != 4 {
			t.Fatalf("invalid bounds: %v %v", bbox, err)
		}

		wanted := 20.
		if index == 1 {
			wanted = 16
		}

		value, err := pdf.DereferenceNumber(bbox[2])
		if err != nil || value != wanted {
			t.Fatalf("shared appearance geometry overwritten: %v %v", value, err)
		}
	}

	if _, reference := buttonDictionary(t, pdf, shared["N"])[buttonOnState].(types.IndirectRef); reference {
		t.Fatal("shared original AP mutated")
	}
}

func buttonStream(t *testing.T, pdf *model.Context, object types.Object) types.StreamDict {
	t.Helper()

	value, err := pdf.Dereference(object)
	if err != nil {
		t.Fatal(err)
	}

	stream, ok := value.(types.StreamDict)
	if !ok {
		t.Fatal("normal appearance is not a stream")
	}

	if err = stream.Decode(); err != nil {
		t.Fatal(err)
	}

	return stream
}

func buttonDictionary(t *testing.T, pdf *model.Context, object types.Object) types.Dict {
	t.Helper()

	dict, err := pdf.DereferenceDict(object)
	if err != nil || dict == nil {
		t.Fatalf("expected dictionary: %v %v", dict, err)
	}

	return dict
}

func TestButtonAnalyzerRefusesUnprovedAndInconsistentSourceShapes(t *testing.T) {
	t.Parallel()

	cases := buttonUnsupportedShapes()
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			dict := buttonTestDictionary()
			change(dict)

			if _, err := compileButtonAppearance(t.Context(), pdf, dict); !errors.Is(err, errFormState) {
				t.Fatalf("unsupported button approved: %v", err)
			}
		})
	}
}

func buttonUnsupportedShapes() map[string]func(types.Dict) {
	return map[string]func(types.Dict){
		"zero border":            func(d types.Dict) { d["BS"] = types.Dict{"S": types.Name("S"), "W": types.Integer(0)} },
		"dashed border":          func(d types.Dict) { d["BS"] = types.Dict{"S": types.Name("D"), "W": types.Integer(1)} },
		"scalar border":          func(d types.Dict) { d["BS"] = types.Integer(1) },
		"border style scalar":    func(d types.Dict) { d["BS"] = types.Dict{"S": types.Integer(1)} },
		"border width scalar":    func(d types.Dict) { d["BS"] = types.Dict{"S": types.Name("S"), "W": types.Name("wide")} },
		"infinite border":        func(d types.Dict) { d["BS"] = types.Dict{"S": types.Name("S"), "W": types.Float(math.Inf(1))} },
		"scalar characteristics": func(d types.Dict) { d["MK"] = types.Integer(1) },
		"rotation":               func(d types.Dict) { d["MK"] = types.Dict{"R": types.Integer(90)} },
		"gray background":        func(d types.Dict) { d["MK"] = types.Dict{"BG": types.NewNumberArray(1)} },
		"invalid background":     func(d types.Dict) { d["MK"] = types.Dict{"BG": types.NewNumberArray(1, 2, 3)} },
		"scalar background":      func(d types.Dict) { d["MK"] = types.Dict{"BG": types.Name("red")} },
		"invalid border color": func(d types.Dict) {
			d["MK"] = types.Dict{"BG": types.NewNumberArray(1, 1, 1), "BC": types.NewNumberArray(0, 0, math.NaN())}
		},
		"unsupported caption": func(d types.Dict) {
			d["MK"] = types.Dict{"BG": types.NewNumberArray(1, 1, 1), "BC": types.NewNumberArray(0, 0, 0), "CA": types.StringLiteral("5")}
		},
		"empty caption": func(d types.Dict) {
			d["MK"] = types.Dict{"BG": types.NewNumberArray(1, 1, 1), "BC": types.NewNumberArray(0, 0, 0), "CA": types.StringLiteral("")}
		},
		"nonsquare":        func(d types.Dict) { d[keyRect] = types.NewNumberArray(0, 0, 20, 30) },
		"scalar rectangle": func(d types.Dict) { d[keyRect] = types.Integer(1) },
		"short rectangle":  func(d types.Dict) { d[keyRect] = types.NewNumberArray(0, 0) },
		"invalid rectangle number": func(d types.Dict) {
			d[keyRect] = types.Array{types.Name("x"), types.Integer(0), types.Integer(20), types.Integer(20)}
		},
		"infinite rectangle":   func(d types.Dict) { d[keyRect] = types.NewNumberArray(0, 0, math.Inf(1), math.Inf(1)) },
		"subtraction overflow": func(d types.Dict) { d[keyRect] = types.NewNumberArray(-1e308, -1e308, 1e308, 1e308) },
		"rounding overflow":    func(d types.Dict) { d[keyRect] = types.NewNumberArray(0, 0, 1e308, 1e308) },
		"too small":            func(d types.Dict) { d[keyRect] = types.NewNumberArray(0, 0, 2, 2) },
		"state mismatch":       func(d types.Dict) { d["AS"] = types.Name(buttonOffState) },
		"unknown state":        func(d types.Dict) { d["AS"] = types.Name("Unknown") },
		"scalar state":         func(d types.Dict) { d["AS"] = types.Integer(1) },
		"string value":         func(d types.Dict) { d["V"] = types.StringLiteral(buttonOnState) },
		"unknown value":        func(d types.Dict) { d["V"] = types.Name("Unknown") },
		"custom DA":            func(d types.Dict) { d["DA"] = types.StringLiteral("/Font 12 Tf") },
		"scalar DA":            func(d types.Dict) { d["DA"] = types.Integer(1) },
		"push button":          func(d types.Dict) { d["Ff"] = types.Integer(buttonPushFlag) },
		"scalar flags":         func(d types.Dict) { d["Ff"] = types.Name("radio") },
		"scalar AP":            func(d types.Dict) { d["AP"] = types.Integer(1) },
		"scalar normal AP":     func(d types.Dict) { d["AP"] = types.Dict{"N": types.Integer(1)} },
		"not state streams": func(d types.Dict) {
			d["AP"] = types.Dict{"N": types.Dict{buttonOffState: types.Dict{}, buttonOnState: types.Dict{}}}
		},
		"no off state": func(d types.Dict) {
			stream := types.StreamDict{Dict: types.Dict{}}
			d["AP"] = types.Dict{"N": types.Dict{"First": stream, "Second": stream}}
		},
		"invalid parent": func(d types.Dict) { d.Delete("Ff"); d[keyParent] = types.Integer(1) },
	}
}

func TestButtonPlansHonorCancellation(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	dict := buttonTestDictionary()

	plan, err := compileButtonAppearance(t.Context(), pdf, dict)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err = compileButtonAppearance(ctx, pdf, dict); !errors.Is(err, context.Canceled) {
		t.Fatalf("analysis cancellation %v", err)
	}

	if err = plan.apply(ctx, pdf, dict); !errors.Is(err, context.Canceled) {
		t.Fatalf("materialization cancellation %v", err)
	}
}

func TestButtonAnalyzerHonorsInheritedRadioSelectionAndNullDefaults(t *testing.T) {
	t.Parallel()

	pdf := signatureTestContext(t)
	dict := buttonTestDictionary()
	parent := types.Dict{"Ff": types.Integer(checkboxRadioFlag), "V": types.Name("Other"), "DA": types.StringLiteral(" ")}
	dict[keyParent] = signatureTestObject(pdf, parent)
	dict["Ff"] = signatureTestObject(pdf, nil)
	dict.Delete("V")
	dict["AS"] = types.Name(buttonOffState)
	characteristics := buttonDictionary(t, pdf, dict["MK"])
	characteristics["CA"] = types.StringLiteral("l")
	characteristics["R"] = types.Integer(0)

	if _, err := compileButtonAppearance(t.Context(), pdf, dict); err != nil {
		t.Fatalf("inherited radio selection refused: %v", err)
	}

	dict["Ff"] = *types.NewIndirectRef(*pdf.Size+1, 0)
	if _, err := compileButtonAppearance(t.Context(), pdf, dict); err != nil {
		t.Fatalf("missing optional flags did not inherit: %v", err)
	}

	parent["V"] = types.Name(buttonOnState)

	dict["AS"] = types.Name(buttonOnState)
	if _, err := compileButtonAppearance(t.Context(), pdf, dict); err != nil {
		t.Fatalf("selected inherited radio refused: %v", err)
	}
}

func TestButtonAnalyzerPropagatesReferencedObjectFailures(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"CA", "normal", "DA", "root DA", "Ff", "V"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			pdf := signatureTestContext(t)
			stream := &types.ObjectStreamDict{}
			stream.Content = []byte("x")
			lazy := types.NewLazyObjectStreamObject(
				stream,
				0,
				1,
				func(context.Context, string) (types.Object, error) { return nil, errBoom },
			)
			ref := signatureTestObject(pdf, lazy)
			dict := buttonTestDictionary()

			switch target {
			case "CA":
				buttonDictionary(t, pdf, dict["MK"])["CA"] = ref
			case "root DA":
				pdf.RootDict[keyAcroForm] = types.Dict{"DA": ref}
			case "normal":
				appearance := buttonDictionary(t, pdf, dict["AP"])
				buttonDictionary(t, pdf, appearance["N"])[buttonOnState] = ref
			default:
				dict[target] = ref
			}

			if _, err := compileButtonAppearance(t.Context(), pdf, dict); !errors.Is(err, errBoom) {
				t.Fatalf("referenced %s failure lost: %v", target, err)
			}
		})
	}
}

func TestButtonInheritanceBoundsAndCancellation(t *testing.T) {
	t.Parallel()

	pdf := signatureTestContext(t)
	dict := types.Dict{}

	dict[keyParent] = signatureTestObject(pdf, dict)
	if _, err := buttonInherited(t.Context(), pdf, dict, "Ff"); !errors.Is(err, errFormState) {
		t.Fatalf("cyclic inheritance accepted: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := buttonDefaultAppearance(ctx, pdf, dict); !errors.Is(err, context.Canceled) {
		t.Fatalf("inheritance cancellation lost: %v", err)
	}

	pdf.RootDict[keyAcroForm] = types.Integer(1)
	if err := buttonDefaultAppearance(t.Context(), pdf, types.Dict{}); !errors.Is(err, errFormState) {
		t.Fatalf("scalar form defaults accepted: %v", err)
	}
}

func TestButtonCompilationCancellationAtEveryCheckpoint(t *testing.T) {
	t.Parallel()

	const checkpoints = 64

	completed := false

	for budget := range checkpoints {
		ctx := newFormCheckpointContext(t.Context(), t)
		ctx.remaining.Store(int64(budget))

		pdf := signatureTestContext(t)

		_, err := compileButtonAppearance(ctx, pdf, buttonTestDictionary())
		if err == nil {
			completed = true
			break
		}

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("button compilation checkpoint %d: %v", budget, err)
		}
	}

	if !completed {
		t.Fatal("bounded button compilation controls never reached success")
	}
}
