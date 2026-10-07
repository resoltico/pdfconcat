// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestRenderingStateCannotHideLayersOutsideCatalogDeclaration(t *testing.T) {
	t.Parallel()

	for _, object := range []types.Object{
		types.Dict{keyType: types.Name("OCG")},
		types.Dict{keyType: types.Name("OCMD")},
		types.Dict{"OC": types.NewIndirectRef(5, 0)},
		types.Array{types.Dict{"OC": types.Name("Layer")}},
		types.StreamDict{Dict: types.Dict{"Resources": types.Dict{"Properties": types.Dict{"L": types.Dict{keyType: types.Name("OCG")}}}}},
	} {
		pdf := &model.Context{
			XRefTable: &model.XRefTable{RootDict: types.Dict{}, Table: map[int]*model.XRefTableEntry{1: {Object: object}}},
		}
		if err := checkRenderingState(pdf); err == nil {
			t.Fatalf("undeclared layered rendering accepted: %v", object)
		}
	}

	for _, key := range []string{"OCProperties", "OutputIntents"} {
		pdf := &model.Context{XRefTable: &model.XRefTable{RootDict: types.Dict{key: types.Dict{}}}}
		if err := checkRenderingState(pdf); err == nil {
			t.Fatalf("unsafe catalog /%s accepted", key)
		}
	}

	pdf := &model.Context{
		XRefTable: &model.XRefTable{
			RootDict: types.Dict{},
			Table: map[int]*model.XRefTableEntry{
				1: nil,
				2: {Free: true, Object: types.Dict{"OC": types.Name("L")}},
				3: {Object: types.Dict{keyType: types.Name("Page"), "N": types.Array{types.Integer(2)}}},
			},
		},
	}
	if err := checkRenderingState(pdf); err != nil {
		t.Fatalf("unlayered source rejected: %v", err)
	}
}

func TestDynamicFormsAreRejectedButStaticCatalogRemainsSupported(t *testing.T) {
	t.Parallel()

	for _, catalog := range []types.Dict{
		{keyNeedsRendering: types.Boolean(true)},
		{keyNeedsRendering: types.Name("false")},
		{keyNeedsRendering: nil},
		{keyAcroForm: types.Dict{"XFA": types.StringLiteral("form XML")}},
		{keyAcroForm: types.StringLiteral("not a dictionary")},
	} {
		pdf := &model.Context{XRefTable: &model.XRefTable{RootDict: catalog}}
		if err := checkRenderingState(pdf); err == nil {
			t.Fatalf("unsupported dynamic/malformed form state accepted: %v", catalog)
		}
	}

	pdf := &model.Context{XRefTable: &model.XRefTable{
		RootDict: types.Dict{keyNeedsRendering: types.Boolean(false), keyAcroForm: *types.NewIndirectRef(1, 0)},
		Table: map[int]*model.XRefTableEntry{
			1: {Object: types.Dict{"Fields": types.Array{}}},
			2: {Object: types.StreamDict{Dict: types.Dict{}, Content: []byte("XFA NeedsRendering OCProperties")}},
		},
	}}
	if err := checkRenderingState(pdf); err != nil {
		t.Fatalf("static form or source stream text rejected: %v", err)
	}
}
