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

const (
	keyAnnots             = "Annots"
	featureAttachmentType = "FileAttachment"
	featurePayloadLabel   = "payload"
)

func featureTestObserver(t *testing.T) *featureObserver {
	t.Helper()
	return &featureObserver{pdf: readUnvalidated(t, pdffixture.Plain("feature boundary")), found: map[FeatureKind]bool{}}
}

// malformedLazyObject reaches the backend's real deferred object-stream decoder with invalid offsets.
func malformedLazyObject(pdf *model.Context) types.IndirectRef {
	stream := &types.ObjectStreamDict{Dict: types.Dict{}, Content: []byte("x"), MaxDecodeBytes: 1024}
	object := types.NewLazyObjectStreamObject(stream, 2, 3, nil)
	entry := model.NewXRefTableEntryGen0(object)
	number := pdf.InsertNew(*entry)

	return *types.NewIndirectRef(number, 0)
}

func TestFeatureNameTreeChildValuesAndActionSequences(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	child := types.Dict{keyNames: types.Array{types.StringLiteral("name"), types.HexLiteral("61")}}
	root := types.Dict{featureKids: types.Array{child}}

	material, err := observer.materialTree(t.Context(), root, keyNames, FeatureOtherCatalogNames, map[types.IndirectRef]bool{}, 0)
	if err != nil || !material {
		t.Fatalf("material name child: %v %v", material, err)
	}

	actions := types.Array{
		types.Dict{"S": types.Name("URI"), "URI": types.StringLiteral("https://example.org")},
		types.Dict{"S": types.Name(featureJavaScript), "JS": types.HexLiteral("74727565")},
	}

	material, err = observer.action(t.Context(), actions, FeaturePageActions, map[types.IndirectRef]bool{}, 0)
	if err != nil || !material {
		t.Fatalf("action sequence: %v %v", material, err)
	}

	observer.pdf.RootDict["OpenAction"] = types.Array{types.Integer(1), types.Name("Fit")}
	if catalogErr := observer.catalog(t.Context()); catalogErr != nil || !observer.found[FeatureCatalogActions] {
		t.Fatalf("catalog destination effect: %v", catalogErr)
	}
}

func TestMalformedLazyFeatureObjectsRemainErrors(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"tree value", "action", "name value", "script", featurePayloadLabel, "catalog open"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			observer := featureTestObserver(t)
			bad := malformedLazyObject(observer.pdf)

			var err error

			switch target {
			case "tree value":
				_, err = observer.materialTree(
					t.Context(),
					types.Dict{keyNames: bad},
					keyNames,
					FeatureOtherCatalogNames,
					map[types.IndirectRef]bool{},
					0,
				)
			case "action":
				_, err = observer.action(t.Context(), bad, FeaturePageActions, map[types.IndirectRef]bool{}, 0)
			case "name value":
				_, err = observer.nameValue(t.Context(), bad, FeatureOtherCatalogNames)
			case "script":
				_, err = observer.materialAction(types.Dict{"S": types.Name(featureJavaScript), "JS": bad}, FeaturePageActions)
			case featurePayloadLabel:
				_, err = observer.attachmentPayload(types.Dict{"EF": types.Dict{"F": bad}})
			case "catalog open":
				observer.pdf.RootDict["OpenAction"] = bad
				err = observer.catalog(t.Context())
			default:
				t.Fatal("unknown fixture target")
			}

			if err == nil {
				t.Fatal("malformed object-stream entry was ignored")
			}
		})
	}
}

func TestMalformedFeatureContainersRejectWithoutInventingObservations(t *testing.T) {
	t.Parallel()

	for _, dict := range []types.Dict{
		{keyNames: types.Dict{"OtherTree": types.Integer(7)}},
		{keyAcroForm: types.Integer(7)},
		{keyAcroForm: types.Dict{keyFields: types.Integer(7)}},
		{keyAcroForm: types.Dict{keyFields: types.Array{types.Integer(7)}}},
		{"PageLabels": types.Dict{featureKids: types.Integer(7)}},
		{"PageLabels": types.Dict{featureKids: types.Array{types.Integer(7)}}},
		{"Pages": types.Integer(7)},
	} {
		observer := featureTestObserver(t)
		maps.Copy(observer.pdf.RootDict, dict)

		if facts, err := observeFeatures(t.Context(), observer.pdf); err == nil {
			t.Fatalf("malformed container accepted: %v facts%v", dict, facts)
		}
	}
}

func TestMalformedAnnotationAndAssociatedFileStateRejects(t *testing.T) {
	t.Parallel()

	for _, page := range []types.Dict{
		{"AA": types.Integer(7)},
		{"AA": types.Dict{"O": types.Dict{"S": types.Integer(7)}}},
		{keyAnnots: types.Integer(7)},
		{keyAnnots: types.Array{types.Integer(7)}},
		{keyAnnots: types.Array{types.Dict{"A": types.Dict{"S": types.Integer(7)}}}},
		{keyAnnots: types.Array{types.Dict{keySubtype: types.Integer(7)}}},
		{keyAnnots: types.Array{types.Dict{keySubtype: types.Name(featureAttachmentType), "FS": types.Integer(7)}}},
		{keyAnnots: types.Array{types.Dict{keySubtype: types.Name(featureAttachmentType), "FS": types.Dict{"EF": types.Integer(7)}}}},
	} {
		observer := featureTestObserver(t)
		if err := observer.page(t.Context(), page); err == nil {
			t.Fatalf("malformed annotation state accepted: %v", page)
		}
	}

	observer := featureTestObserver(t)
	if err := observer.associatedFiles(t.Context(), types.Array{types.Integer(7)}); err == nil {
		t.Fatal("malformed associated file accepted")
	}

	if material, err := observer.attachmentPayload(types.Dict{}); err != nil || material {
		t.Fatalf("external filespec invented embedded bytes: %v %v", material, err)
	}
}

func TestFeatureTraversalCancellationAndChildGuards(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for _, run := range []func() error{
		func() error { _, err := observer.nameValue(ctx, nil, FeatureOtherCatalogNames); return err },
		func() error { return observer.annotation(ctx, nil) },
		func() error {
			return observer.fieldActions(ctx, types.Array{types.Dict{}}, map[types.IndirectRef]bool{}, 0)
		},
		func() error { return observer.associatedFiles(ctx, types.Array{types.Dict{}}) },
	} {
		if err := run(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	}

	_, treeErr := observer.materialTree(
		t.Context(),
		nil,
		keyNames,
		FeatureOtherCatalogNames,
		nil,
		maxFeatureDepth+1,
	)
	if !errors.Is(treeErr, errTreeTooDeep) {
		t.Fatalf("name depth guard: %v", treeErr)
	}

	if err := observer.fieldActions(t.Context(), nil, nil, maxFeatureDepth+1); !errors.Is(err, errTreeTooDeep) {
		t.Fatalf("field depth guard: %v", err)
	}

	for _, field := range []types.Dict{{"AA": types.Integer(7)}, {featureKids: types.Integer(7)}} {
		if err := observer.fieldActions(t.Context(), types.Array{field}, map[types.IndirectRef]bool{}, 0); err == nil {
			t.Fatalf("field action container ignored: %v", field)
		}
	}
}

func TestScriptDataSemanticsAndDecodeFailures(t *testing.T) {
	t.Parallel()

	for _, object := range []types.Object{
		types.HexLiteral("zz"),
		types.StreamDict{Dict: types.Dict{}, Raw: []byte("bad"), FilterPipeline: []types.PDFFilter{{Name: "FlateDecode"}}},
	} {
		if material, err := materialScript(object); err == nil {
			t.Fatalf("undecodable script treated as material=%v", material)
		}
	}

	if material, err := materialScript(types.Integer(7)); err != nil || material {
		t.Fatalf("non-script object became active content: %v %v", material, err)
	}

	for _, value := range []types.Object{types.StringLiteral("named"), types.HexLiteral("61"), types.StreamDict{Content: []byte("named")}} {
		observer := featureTestObserver(t)
		if material, err := observer.nameValue(t.Context(), value, FeatureOtherCatalogNames); err != nil || !material {
			t.Fatalf("named value lost: %v %v", material, err)
		}
	}

	observer := featureTestObserver(t)

	material, actionErr := observer.action(
		t.Context(),
		types.Array{types.Dict{"S": types.Integer(7)}},
		FeaturePageActions,
		nil,
		0,
	)
	if actionErr == nil || material {
		t.Fatalf("bad sequence action ignored: %v %v", material, actionErr)
	}

	material, actionErr = observer.action(t.Context(), types.Dict{}, FeaturePageActions, nil, 0)
	if actionErr != nil || material {
		t.Fatalf("empty action warned: %v %v", material, actionErr)
	}
}

func TestNameTreeDecodeFailureAndFieldCycleCannotBeSkipped(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	bad := malformedLazyObject(observer.pdf)

	tree := types.Dict{keyNames: types.Array{types.StringLiteral("broken"), bad}}
	if material, err := observer.materialTree(t.Context(), tree, keyNames, FeatureOtherCatalogNames, nil, 0); err == nil || material {
		t.Fatalf("bad name entry ignored: %v %v", material, err)
	}

	field := types.Dict{}
	reference := signatureTestObject(observer.pdf, field)

	field[featureKids] = types.Array{reference}

	fieldErr := observer.fieldActions(
		t.Context(),
		types.Array{reference},
		map[types.IndirectRef]bool{},
		0,
	)
	if !errors.Is(fieldErr, errNodeRepeated) {
		t.Fatalf("field action cycle: %v", fieldErr)
	}
}
