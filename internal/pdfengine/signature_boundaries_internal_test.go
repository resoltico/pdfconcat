// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestSignatureMalformedContainersStayFatal(t *testing.T) {
	t.Parallel()

	for _, prepare := range []func(*testing.T){
		func(t *testing.T) {
			t.Helper()
			pdf := signatureTestContext(t)

			pdf.RootDict[keyAcroForm] = types.Integer(7)
			if err := checkSignatureState(t.Context(), pdf); err == nil {
				t.Fatal("bad form lookup accepted")
			}
		},
		func(t *testing.T) {
			t.Helper()
			pdf := signatureTestContext(t)

			walker := signatureWalker{pdf: pdf, seen: map[int]bool{}}
			if err := walker.fields(t.Context(), types.Integer(7), "", 0); err == nil {
				t.Fatal("bad fields array accepted")
			}
		},
		func(t *testing.T) {
			t.Helper()
			pdf := signatureTestContext(t)

			walker := signatureWalker{pdf: pdf, seen: map[int]bool{}}
			if err := walker.field(t.Context(), types.Integer(7), "", 0); err == nil {
				t.Fatal("bad field dictionary accepted")
			}
		},
		func(t *testing.T) {
			t.Helper()
			pdf := signatureTestContext(t)

			walker := signatureWalker{pdf: pdf, seen: map[int]bool{}}
			if err := walker.field(t.Context(), types.Dict{"FT": types.Integer(7)}, "", 0); !errors.Is(err, errFormState) {
				t.Fatalf("bad field type: %v", err)
			}
		},
	} {
		prepare(t)
	}
}

func TestSignatureValueObjectDecoderErrorsRemainVisible(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)

	bad := malformedLazyObject(pdf)
	if err := checkSignatureValue(pdf, types.Dict{"V": bad}, "Tx"); err == nil {
		t.Fatal("failed signature-value decode accepted")
	}

	if err := checkSignatureDictionary(pdf, types.Dict{keyType: types.Integer(7)}); err == nil {
		t.Fatal("malformed value type accepted")
	}

	if kind, err := fieldKind(pdf, types.Dict{"FT": nil}, "Tx"); err != nil || kind != "Tx" {
		t.Fatalf("null field type changed inheritance: %q %v", kind, err)
	}
}

func TestSignatureWidgetTypeAndParentFailuresStayFatal(t *testing.T) {
	t.Parallel()

	pdf := signatureTestContext(t)
	for _, page := range []types.Dict{
		{keyAnnots: types.Integer(7)},
		{keyAnnots: types.Array{types.Integer(7)}},
		{keyAnnots: types.Array{types.Dict{keySubtype: types.Integer(7)}}},
	} {
		if err := checkWidgetSignatures(t.Context(), pdf, page); err == nil {
			t.Fatalf("bad widget state accepted: %v", page)
		}
	}

	if _, err := effectiveWidgetKind(t.Context(), pdf, types.Dict{keyParent: types.Integer(7)}); err == nil {
		t.Fatal("bad inherited parent accepted")
	}

	if err := checkWidgetParents(t.Context(), pdf, types.Dict{"FT": types.Name("Tx"), keyParent: types.Integer(7)}); err == nil {
		t.Fatal("bad widget parent accepted")
	}

	parent := types.Dict{"FT": types.Integer(7)}

	ref := signatureTestObject(pdf, parent)
	if err := checkWidgetParents(t.Context(), pdf, types.Dict{"FT": types.Name("Tx"), keyParent: ref}); err == nil {
		t.Fatal("parent field type failure concealed")
	}

	if kind, err := effectiveWidgetKind(t.Context(), pdf, types.Dict{}); err != nil || kind != "" {
		t.Fatalf("unsigned widget without field type: %q %v", kind, err)
	}
}

func TestSignatureDepthAndCancellationPreserveClassification(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)

	walker := signatureWalker{pdf: pdf, seen: map[int]bool{}}
	if err := walker.fields(t.Context(), types.Array{types.Dict{}}, "", maxFieldDepth+1); !errors.Is(err, errFormState) {
		t.Fatalf("field depth: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := walker.fields(ctx, nil, "", 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("field cancellation: %v", err)
	}

	if _, err := effectiveWidgetKind(ctx, pdf, types.Dict{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("inherited-type cancellation: %v", err)
	}

	parent := types.Dict{}
	ref := signatureTestObject(pdf, parent)

	parent[keyParent] = ref
	if _, err := effectiveWidgetKind(t.Context(), pdf, parent); !errors.Is(err, errFormState) {
		t.Fatalf("inherited parent cycle: %v", err)
	}

	parent["FT"] = types.Name("Tx")
	if err := checkWidgetParents(t.Context(), pdf, parent); !errors.Is(err, errFormState) {
		t.Fatalf("widget parent cycle: %v", err)
	}
}

func TestWidgetCancellationAfterInheritedTypeLookupStopsTraversal(t *testing.T) {
	t.Parallel()
	pdf := signatureTestContext(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stream := &types.ObjectStreamDict{Dict: types.Dict{}, Content: []byte("/Tx"), MaxDecodeBytes: 1024}
	object := types.NewLazyObjectStreamObject(
		stream,
		0,
		3,
		func(context.Context, string) (types.Object, error) { cancel(); return types.Name("Tx"), nil },
	)

	reference := signatureTestObject(pdf, object)
	if err := checkWidgetParents(ctx, pdf, types.Dict{"FT": reference}); !errors.Is(err, context.Canceled) {
		t.Fatalf("inherited lookup cancellation lost: %v", err)
	}
}
