// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type formFixture struct {
	pdf                  *model.Context
	form, parent, widget types.Dict
}

const (
	type1FontSubtype = "Type1"
	procedurePDF     = "PDF"
	procedureText    = "Text"
)

func formTestContext() *formFixture {
	font := types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name(type1FontSubtype), keyBaseFont: types.Name(fontHelvetica)}
	parent := types.Dict{"FT": types.Name("Tx"), "T": types.StringLiteral("group"), "Kids": types.Array{*types.NewIndirectRef(2, 0)}}
	widget := types.Dict{keySubtype: types.Name(widgetSubtype), keyParent: *types.NewIndirectRef(1, 0), "T": types.StringLiteral("value")}
	form := types.Dict{
		keyFields: types.Array{*types.NewIndirectRef(1, 0)},
		"DA":      types.StringLiteral("/F1 12 Tf 0.1 0.2 0.3 rg"),
		"Q":       types.Integer(2),
		"DR":      types.Dict{keyFont: types.Dict{"F1": font}},
	}
	conf := &model.Configuration{Limits: model.DefaultResourceLimits()}
	pdf := &model.Context{Configuration: conf, XRefTable: &model.XRefTable{
		Conf:     conf,
		RootDict: types.Dict{keyAcroForm: form},
		Table:    map[int]*model.XRefTableEntry{1: model.NewXRefTableEntryGen0(parent), 2: model.NewXRefTableEntryGen0(widget)},
	}}

	return &formFixture{pdf: pdf, form: form, parent: parent, widget: widget}
}

func TestFormNormalizationPreservesEffectiveInheritedDefaultsAndIndependentScopes(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	pdf := fixture.pdf
	appearanceFonts := types.Dict{"F1": types.Name("appearance-font")}
	appearance := types.Dict{"N": types.Dict{"Resources": types.Dict{keyFont: appearanceFonts}}}
	fixture.widget["AP"] = appearance

	state, err := prepareFormResources(t.Context(), pdf, 7)
	if err != nil {
		t.Fatal(err)
	}

	if state == nil {
		t.Fatal("missing form state")
	}

	for _, number := range []int{1, 2} {
		dict, readErr := pdf.DereferenceDict(*types.NewIndirectRef(number, 0))
		if readErr != nil {
			t.Fatal(readErr)
		}

		text, readErr := pdf.DereferenceStringEntryBytes(dict, "DA")
		if readErr != nil {
			t.Fatal(readErr)
		}

		if string(text) != "/Form7_Font1 12 Tf 0.1 0.2 0.3 rg" || dict["Q"] != types.Integer(2) {
			t.Fatalf("field defaults: %+v", dict)
		}
	}

	if appearanceFonts["F1"] != types.Name("appearance-font") {
		t.Fatal("independent AP resource scope was renamed")
	}
}

func TestFormStateRejectsAmbiguousOrUnsupportedFieldShapes(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*formFixture){
		"shared": func(fixture *formFixture) {
			fixture.form[keyFields] = types.Array{*types.NewIndirectRef(1, 0), *types.NewIndirectRef(1, 0)}
		},
		"cycle": func(fixture *formFixture) {
			fixture.widget["Kids"] = types.Array{*types.NewIndirectRef(1, 0)}
		},
		"parent mismatch":       func(fixture *formFixture) { fixture.widget[keyParent] = *types.NewIndirectRef(8, 0) },
		"direct field":          func(fixture *formFixture) { fixture.form[keyFields] = types.Array{types.Dict{}} },
		"field resources":       func(fixture *formFixture) { fixture.widget["DR"] = types.Dict{} },
		"missing font":          func(fixture *formFixture) { fixture.form["DR"] = types.Dict{} },
		"non-string appearance": func(fixture *formFixture) { fixture.widget["DA"] = types.Integer(1) },
		"justification":         func(fixture *formFixture) { fixture.widget["Q"] = types.Integer(3) },
		"field type":            func(fixture *formFixture) { fixture.widget["FT"] = types.Integer(1) },
		"fontless text":         func(fixture *formFixture) { fixture.form["DA"] = types.StringLiteral("0 g") },
		"depth":                 func(fixture *formFixture) { fixture.pdf.Conf.Limits.MaxRecursionDepth = 1 },
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := formTestContext()
			pdf := fixture.pdf
			damage(fixture)

			if err := checkFormState(t.Context(), pdf); !errors.Is(err, errFormState) {
				t.Fatalf("invalid shape accepted: %v", err)
			}
		})
	}
}

func TestFormStateCancellationAndEmptySignatureField(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	pdf := fixture.pdf
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := checkFormState(ctx, pdf); !errors.Is(err, context.Canceled) {
		t.Fatalf("form_state_internal_test cancellation: %v", err)
	}

	fixture.parent["FT"] = types.Name(signatureType)
	fixture.form.Delete("DA")

	if err := checkFormState(t.Context(), pdf); err != nil {
		t.Fatal(err)
	}
}

func TestFormResourcesRejectUnsupportedBindings(t *testing.T) {
	t.Parallel()

	for _, category := range []string{keyFont, keyExtGState, "XObject", "Pattern", "ColorSpace", keyProperties, "Other"} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			fixture := formTestContext()
			pdf := fixture.pdf
			form := fixture.form

			form["DR"] = types.Dict{
				keyFont:  types.Dict{"F1": types.Dict{keyType: types.Name(keyFont), keySubtype: types.Name(type1FontSubtype)}},
				category: types.Dict{"bad": types.Integer(42)},
			}
			if err := checkFormState(t.Context(), pdf); err == nil || !strings.Contains(err.Error(), category) {
				t.Fatalf("invalid %s binding: %v", category, err)
			}
		})
	}
}

func TestFormResourcesPreserveAuxiliaryEncodingBindings(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	encoding := types.Dict{keyType: types.Name(keyEncoding), keyDifferences: types.Array{types.Integer(65), types.Name("W")}}
	font := types.Dict{
		keyType:     types.Name(keyFont),
		keySubtype:  types.Name(type1FontSubtype),
		keyBaseFont: types.Name(fontHelvetica),
		keyEncoding: encoding,
	}
	fixture.form["DR"] = types.Dict{keyFont: types.Dict{"F1": font}, keyEncoding: types.Dict{"RLAFencoding": encoding}}

	state, err := prepareFormResources(t.Context(), fixture.pdf, 2)
	if err != nil {
		t.Fatal(err)
	}

	if state == nil {
		t.Fatal("missing normalized form")
	}
	// Changing the retained font binding's encoding exposes accidental copies or dropped bindings.
	encoding["BaseEncoding"] = types.Name(nameWinAnsiEncoding)

	resources, err := fixture.pdf.DereferenceDict(fixture.form["DR"])
	if err != nil {
		t.Fatal(err)
	}

	bindings, err := fixture.pdf.DereferenceDict(resources[keyEncoding])
	if err != nil {
		t.Fatal(err)
	}

	retained, err := fixture.pdf.DereferenceDict(bindings["Form2_Encoding1"])
	if err != nil || retained["BaseEncoding"] != types.Name(nameWinAnsiEncoding) {
		t.Fatalf("auxiliary encoding binding lost: %+v %v", retained, err)
	}
}

func TestFormResourceAndDefaultShapeFailuresAreRejected(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*formFixture){
		"root is scalar":          func(f *formFixture) { f.pdf.RootDict[keyAcroForm] = types.Integer(1) },
		"fields are scalar":       func(f *formFixture) { f.form[keyFields] = types.Integer(1) },
		"resources are scalar":    func(f *formFixture) { f.form["DR"] = types.Integer(1) },
		"font category is scalar": func(f *formFixture) { f.form["DR"] = types.Dict{keyFont: types.Integer(1)} },
		"font category is null":   func(f *formFixture) { f.form["DR"] = types.Dict{keyFont: nil} },
		"appearance is null":      func(f *formFixture) { f.parent["DA"] = nil },
		"Q reference is invalid":  func(f *formFixture) { f.parent["Q"] = types.Dict{} },
		"field is scalar":         func(f *formFixture) { f.pdf.Table[1].Object = types.Integer(1) },
		"field is null":           func(f *formFixture) { f.pdf.Table[1].Object = nil },
		"Kids is scalar":          func(f *formFixture) { f.parent["Kids"] = types.Integer(1) },
		"root Parent is present":  func(f *formFixture) { f.parent[keyParent] = *types.NewIndirectRef(2, 0) },
		"ProcSet is scalar":       func(f *formFixture) { f.form["DR"] = types.Dict{keyProcSet: types.Integer(1)} },
		"ProcSet contains scalar": func(f *formFixture) { f.form["DR"] = types.Dict{keyProcSet: types.Array{types.Integer(1)}} },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := formTestContext()
			corrupt(fixture)

			if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
				t.Fatalf("malformed form accepted: %v", err)
			}
		})
	}
}

func TestFormResourceShapesAndProcSetMerge(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	for category, object := range map[string]types.Object{
		keyExtGState: types.Dict{}, keyProperties: types.Dict{}, "XObject": types.StreamDict{},
		"Pattern": types.Dict{}, "ColorSpace": types.Name("DeviceRGB"), keyEncoding: types.Dict{keyType: types.Name(keyEncoding)},
	} {
		if err := checkFormResource(t.Context(), fixture.pdf, category, object); err != nil {
			t.Fatalf("supported %s resource refused: %v", category, err)
		}
	}

	resources := types.Dict{keyProcSet: types.Array{types.Name(procedurePDF), types.Name(procedureText)}}

	mergeErr := mergeFormProcSet(
		t.Context(),
		fixture.pdf,
		resources,
		types.Array{types.Name(procedureText), types.Name("ImageB")},
	)
	if mergeErr != nil {
		t.Fatal(mergeErr)
	}

	merged, err := fixture.pdf.DereferenceArray(resources[keyProcSet])
	if err != nil || len(merged) != 3 {
		t.Fatalf("procedure names not merged: %v %v", merged, err)
	}

	if importErr := mergeFormProcSet(t.Context(), fixture.pdf, resources, types.Integer(1)); importErr == nil {
		t.Fatal("invalid imported ProcSet accepted")
	}

	destinationErr := mergeFormProcSet(
		t.Context(),
		fixture.pdf,
		types.Dict{keyProcSet: types.Integer(1)},
		types.Array{},
	)
	if destinationErr == nil {
		t.Fatal("invalid destination ProcSet accepted")
	}

	font := types.Dict{keyType: types.Name(keyFont)}
	if fontErr := checkFormFont(t.Context(), fixture.pdf, font); fontErr == nil {
		t.Fatal("font without subtype accepted")
	}

	font[keySubtype] = types.Integer(1)
	if fontErr := checkFormFont(t.Context(), fixture.pdf, font); fontErr == nil {
		t.Fatal("invalid font subtype accepted")
	}
}

func TestFormRegenerationRequestsStayScopedToVariableWidgets(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Tx", "Ch"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			fixture := formTestContext()
			fixture.parent["FT"] = types.Name(kind)
			fixture.form["NeedAppearances"] = types.Boolean(true)

			fixture.widget["AP"] = types.Dict{"N": types.Dict{}}
			if _, err := prepareFormResources(t.Context(), fixture.pdf, 1); err != nil {
				t.Fatal(err)
			}

			_, retained := fixture.widget.Find("AP")
			if retained {
				t.Fatalf("%s AP scope: retained=%t", kind, retained)
			}

			if fixture.form["NeedAppearances"] != types.Boolean(false) {
				t.Fatal("source regeneration flag leaked into merged global state")
			}
		})
	}

	fixture := formTestContext()

	fixture.form["NeedAppearances"] = types.Integer(1)
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
		t.Fatalf("malformed regeneration state accepted: %v", err)
	}
}

func TestVariableRegenerationRejectsAdditionalAppearanceModesWithoutChangingThem(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"Tx", "Ch"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			for _, mode := range []string{"D", "R", "AdditionalMode"} {
				for _, value := range []types.Object{nil, *types.NewIndirectRef(10, 0)} {
					verifyAlternativeAppearanceRefusal(t, kind, mode, value)
				}
			}
		})
	}
}

func TestUnrequestedVariableRegenerationPreservesAlternativeAppearanceReferences(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	normal, down, rollover := *types.NewIndirectRef(10, 0), *types.NewIndirectRef(11, 0), *types.NewIndirectRef(12, 0)
	shared := types.Dict{"N": normal, "D": down, "R": rollover}

	fixture.widget["AP"] = shared
	if _, err := prepareFormResources(t.Context(), fixture.pdf, 1); err != nil {
		t.Fatal(err)
	}

	appearance, err := fixture.pdf.DereferenceDict(fixture.widget["AP"])
	if err != nil || appearance["N"] != normal || appearance["D"] != down || appearance["R"] != rollover {
		t.Fatalf("alternative appearances changed: %v %v", appearance, err)
	}
}

func TestVariableRegenerationRejectsMalformedAppearanceDictionary(t *testing.T) {
	t.Parallel()

	fixture := formTestContext()
	fixture.form["NeedAppearances"] = types.Boolean(true)

	fixture.widget["AP"] = types.Integer(1)
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
		t.Fatalf("scalar variable-widget AP accepted: %v", err)
	}

	fixture.parent["FT"] = types.Name(signatureType)
	fixture.widget.Delete("AP")

	if err := checkFormState(t.Context(), fixture.pdf); err != nil {
		t.Fatalf("empty unsigned signature regeneration flag rejected: %v", err)
	}
}

func verifyAlternativeAppearanceRefusal(t *testing.T, kind, mode string, value types.Object) {
	t.Helper()

	fixture := formTestContext()
	fixture.parent["FT"] = types.Name(kind)
	fixture.form["NeedAppearances"] = types.Boolean(true)
	appearance := types.Dict{"N": *types.NewIndirectRef(11, 0), mode: value}

	fixture.widget["AP"] = appearance
	if err := checkFormState(t.Context(), fixture.pdf); !errors.Is(err, errFormState) {
		t.Fatalf("additional appearance accepted: %s %v", mode, err)
	}

	if len(appearance) != 2 || appearance[mode] != value {
		t.Fatal("refusal changed source appearances")
	}
}
