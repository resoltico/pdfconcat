// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func signatureTestContext(t *testing.T) *model.Context {
	t.Helper()

	pdf := readUnvalidated(t, pdffixture.Plain("UNSIGNED text /Sig /ByteRange"))
	if _, err := pdf.Catalog(); err != nil {
		t.Fatal(err)
	}

	return pdf
}

func signatureTestObject(pdf *model.Context, object types.Object) types.IndirectRef {
	entry := model.NewXRefTableEntryGen0(object)
	entry.RefCount = 1
	number := pdf.InsertNew(*entry)

	return *types.NewIndirectRef(number, 0)
}

func signatureTestField(pdf *model.Context, field types.Dict) types.IndirectRef {
	ref := signatureTestObject(pdf, field)
	pdf.RootDict[keyAcroForm] = types.Dict{keyFields: types.Array{ref}}

	return ref
}

func signatureTestWidget(t *testing.T, pdf *model.Context, widget types.Dict) {
	t.Helper()

	ref, err := pdf.Pages()
	if err != nil {
		t.Fatal(err)
	}

	walkErr := walkPages(t.Context(), pdf, *ref, func(_ types.IndirectRef, page types.Dict, _ inheritedAttrs) error {
		page["Annots"] = types.Array{signatureTestObject(pdf, widget)}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
}

func TestSignatureWalkerAcceptsLastDepthAndEmptyTerminalChildren(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	walker := signatureWalker{pdf: pdf, seen: map[int]bool{}}
	if err := walker.fields(t.Context(), types.Array{}, "", maxFieldDepth+1); err != nil {
		t.Fatalf("empty terminal children refused: %v", err)
	}
	if err := walker.fields(t.Context(), types.Array{types.Dict{}}, "", maxFieldDepth); err != nil {
		t.Fatalf("last permitted field depth refused: %v", err)
	}
	if err := walker.fields(t.Context(), types.Array{types.Dict{}}, "", maxFieldDepth+1); !errors.Is(err, errFormState) {
		t.Fatalf("field depth beyond limit accepted: %v", err)
	}
	field := types.Dict{}
	for range maxFieldDepth + 1 {
		field = types.Dict{"Kids": types.Array{signatureTestObject(pdf, field)}}
	}
	if err := walker.fields(t.Context(), types.Array{field}, "", 0); !errors.Is(err, errFormState) {
		t.Fatalf("actual nested field tree exceeded depth without refusal: %v", err)
	}
}

func TestSignatureWidgetParentLimitAndAnnotationFiltering(t *testing.T) {
	// Settle finite depth assertions before parallel cyclic fixtures begin.
	for _, depth := range []int{maxFieldDepth, maxFieldDepth + 1} {
		pdf := signatureTestContext(t)
		field := types.Dict{}
		for range depth {
			field = types.Dict{keyParent: signatureTestObject(pdf, field)}
		}
		_, kindErr := effectiveWidgetKind(t.Context(), pdf, field)
		if depth == maxFieldDepth && kindErr != nil || depth > maxFieldDepth && !errors.Is(kindErr, errFormState) {
			t.Fatalf("inherited kind depth %d: %v", depth, kindErr)
		}
		field["FT"] = types.Name(buttonFieldType)
		err := checkWidgetParents(t.Context(), pdf, field)
		if depth == maxFieldDepth && err != nil || depth > maxFieldDepth && !errors.Is(err, errFormState) {
			t.Fatalf("parent depth %d: %v", depth, err)
		}
	}
	pdf := signatureTestContext(t)
	for _, annotation := range []types.Dict{{}, {keySubtype: types.Name("Link")}} {
		annotation["FT"] = types.Name(signatureType)
		annotation["V"] = types.Dict{}
		if err := checkWidgetSignatures(t.Context(), pdf, types.Dict{"Annots": types.Array{annotation}}); err != nil {
			t.Fatalf("non-widget annotation inspected as signature field: %v", err)
		}
		widget := types.Dict{keySubtype: types.Name(widgetSubtype), "FT": types.Name(signatureType), "V": types.Dict{}}
		if err := checkWidgetSignatures(t.Context(), pdf, types.Dict{"Annots": types.Array{annotation, widget}}); !errors.Is(err, errSignatureState) {
			t.Fatalf("non-widget annotation hid a later signed widget: %v", err)
		}
	}
}

func TestSignatureFieldAllowsOnlyActualNullValue(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	null := signatureTestObject(pdf, nil)
	if err := checkPopulatedSignature(t.Context(), pdf, null); err != nil {
		t.Fatalf("existing null signature value refused: %v", err)
	}
	broken := failedVariableObject(&formFixture{pdf: pdf})
	if err := checkPopulatedSignature(t.Context(), pdf, broken); !errors.Is(err, errSignatureState) {
		t.Fatalf("unreadable populated signature reference accepted: %v", err)
	}
}

func TestSignatureStateRejectsReachableSignedAndMalformedValues(t *testing.T) {
	t.Parallel()

	cases := signedValueCases()
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pdf := signatureTestContext(t)
			prepare(t, pdf)

			if err := checkSignatureState(t.Context(), pdf); !errors.Is(err, errSignatureState) {
				t.Fatalf("reachable signature accepted: %v", err)
			}
		})
	}
}

func signedValueCases() map[string]func(*testing.T, *model.Context) {
	cases := signedFieldCases()
	maps.Copy(cases, signedWidgetCases())
	maps.Copy(cases, signedCatalogCases())

	return cases
}

func signedFieldCases() map[string]func(*testing.T, *model.Context) {
	return map[string]func(*testing.T, *model.Context){
		"signature field": func(_ *testing.T, pdf *model.Context) {
			signatureTestField(pdf, types.Dict{"FT": types.Name(signatureType), "V": types.Dict{keyType: types.Name(signatureType)}})
		},
		"scalar signature value": func(_ *testing.T, pdf *model.Context) {
			signatureTestField(pdf, types.Dict{"FT": types.Name(signatureType), "V": types.Integer(42)})
		},
		"broken signature reference": func(_ *testing.T, pdf *model.Context) {
			signatureTestField(pdf, types.Dict{"FT": types.Name(signatureType), "V": *types.NewIndirectRef(999, 0)})
		},
		"timestamp value": func(_ *testing.T, pdf *model.Context) {
			signatureTestField(pdf, types.Dict{"FT": types.Name(buttonFieldType), "V": types.Dict{keyType: types.Name("DocTimeStamp")}})
		},
		"inherited field": func(_ *testing.T, pdf *model.Context) {
			child := signatureTestObject(pdf, types.Dict{"V": types.Dict{}})
			signatureTestField(pdf, types.Dict{"FT": types.Name(signatureType), "Kids": types.Array{child}})
		},
		"indirect signature type": func(_ *testing.T, pdf *model.Context) {
			kind := signatureTestObject(pdf, types.Name(signatureType))
			signatureTestField(pdf, types.Dict{"FT": types.Name(buttonFieldType), "V": types.Dict{keyType: kind}})
		},
	}
}

func signedWidgetCases() map[string]func(*testing.T, *model.Context) {
	return map[string]func(*testing.T, *model.Context){
		"detached widget": func(t *testing.T, pdf *model.Context) {
			t.Helper()

			parent := signatureTestObject(pdf, types.Dict{"FT": types.Name(signatureType)})
			signatureTestWidget(t, pdf, types.Dict{keySubtype: types.Name(widgetSubtype), keyParent: parent, "V": types.Dict{}})
		},
		"indirect widget subtype": func(t *testing.T, pdf *model.Context) {
			t.Helper()

			subtype := signatureTestObject(pdf, types.Name(widgetSubtype))
			signatureTestWidget(t, pdf, types.Dict{keySubtype: subtype, "FT": types.Name(signatureType), "V": types.Dict{}})
		},
		"detached ByteRange without type": func(t *testing.T, pdf *model.Context) {
			t.Helper()
			signatureTestWidget(
				t,
				pdf,
				types.Dict{
					keySubtype: types.Name(widgetSubtype),
					"V":        types.Dict{"ByteRange": types.Array{types.Integer(0), types.Integer(10)}},
				},
			)
		},
		"detached Contents with other field type": func(t *testing.T, pdf *model.Context) {
			t.Helper()
			signatureTestWidget(
				t,
				pdf,
				types.Dict{
					keySubtype: types.Name(widgetSubtype),
					"FT":       types.Name(buttonFieldType),
					"V":        types.Dict{"Contents": types.HexLiteral("00")},
				},
			)
		},
		"detached Contents malformed field type": func(t *testing.T, pdf *model.Context) {
			t.Helper()
			signatureTestWidget(
				t,
				pdf,
				types.Dict{
					keySubtype: types.Name(widgetSubtype),
					"FT":       types.Integer(42),
					"V":        types.Dict{"Contents": types.HexLiteral("00")},
				},
			)
		},
	}
}

func signedCatalogCases() map[string]func(*testing.T, *model.Context) {
	return map[string]func(*testing.T, *model.Context){
		"DocMDP":                func(_ *testing.T, pdf *model.Context) { pdf.RootDict["Perms"] = types.Dict{"DocMDP": types.Dict{}} },
		"usage rights":          func(_ *testing.T, pdf *model.Context) { pdf.RootDict["Perms"] = types.Dict{"UR": types.Dict{}} },
		"usage rights UR3":      func(_ *testing.T, pdf *model.Context) { pdf.RootDict["Perms"] = types.Dict{"UR3": types.Dict{}} },
		"malformed permissions": func(_ *testing.T, pdf *model.Context) { pdf.RootDict["Perms"] = types.Integer(42) },
	}
}

func TestSignatureStateAcceptsUnsignedFieldsAndUnreferencedDecoys(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	signatureTestField(pdf, types.Dict{"FT": types.Name(signatureType)})
	signatureTestObject(pdf, types.Dict{keyType: types.Name(signatureType), "ByteRange": types.Array{types.Integer(0), types.Integer(10)}})

	if err := checkSignatureState(t.Context(), pdf); err != nil {
		t.Fatal(err)
	}

	signatureTestField(pdf, types.Dict{"FT": types.Name(signatureType), "V": nil})

	if err := checkSignatureState(t.Context(), pdf); err != nil {
		t.Fatal(err)
	}
}

func TestSignatureStateRejectsCyclesAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	parent := types.Dict{"FT": types.Name(signatureType)}
	ref := signatureTestField(pdf, parent)
	parent["Kids"] = types.Array{ref}

	if err := checkSignatureState(t.Context(), pdf); !errors.Is(err, errFormState) {
		t.Fatalf("field cycle: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := checkSignatureState(ctx, pdf); !errors.Is(err, context.Canceled) {
		t.Fatalf("signature traversal cancellation: %v", err)
	}
}

func TestSignatureStateRejectsMalformedWidgetFieldType(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)
	parent := signatureTestObject(pdf, types.Dict{"FT": types.Name(signatureType)})
	signatureTestWidget(t, pdf, types.Dict{
		keySubtype: types.Name(widgetSubtype), keyParent: parent, "FT": types.Integer(42), "V": types.Dict{},
	})

	if err := checkSignatureState(t.Context(), pdf); !errors.Is(err, errFormState) {
		t.Fatalf("malformed field type obscured inherited signature state: %v", err)
	}
}
