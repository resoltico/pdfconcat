// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
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
	root := types.Dict{keyKids: types.Array{child}}

	material, err := observer.materialTree(t.Context(), root, keyNames, FeatureOtherCatalogNames, map[types.IndirectRef]bool{}, 0)
	if err != nil || !material {
		t.Fatalf("material name child: %v %v", material, err)
	}

	actions := types.Array{
		types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral("https://example.org")},
		types.Dict{"S": types.Name(featureJavaScript), "JS": types.HexLiteral("74727565")},
	}

	material, err = observer.action(t.Context(), actions, FeaturePageActions, map[types.IndirectRef]bool{}, 0)
	if err != nil || !material {
		t.Fatalf("action sequence: %v %v", material, err)
	}

	observer.pdf.RootDict["OpenAction"] = types.Array{types.Integer(1), types.Name(fitDestinationMode)}
	if catalogErr := observer.catalog(t.Context()); catalogErr != nil || !observer.found[FeatureCatalogActions] {
		t.Fatalf("catalog destination effect: %v", catalogErr)
	}
}

func TestNonDestinationNameTreesUsePairedValues(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	names := types.Dict{
		keyDests: types.Integer(7), // Destination names are retained by assembly, not reported as removed.
		"Renditions": types.Dict{keyNames: types.Array{
			types.StringLiteral("material key"), types.StringLiteral(""),
			types.StringLiteral("empty key"), types.StringLiteral("material value"),
		}},
	}
	expected := types.Dict{
		keyDests: types.Integer(7),
		"Renditions": types.Dict{keyNames: types.Array{
			types.StringLiteral("material key"), types.StringLiteral(""),
			types.StringLiteral("empty key"), types.StringLiteral("material value"),
		}},
	}
	if err := observer.nameTrees(t.Context(), names); err != nil {
		t.Fatal(err)
	}
	if !observer.found[FeatureOtherCatalogNames] {
		t.Fatal("non-destination name tree lost its material second value")
	}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("name observation modified the parsed model dictionary: %v", names)
	}

	for _, entries := range []types.Array{
		{},
		{types.StringLiteral("material key"), types.StringLiteral("")},
	} {
		material, err := observer.treeValue(t.Context(), entries, keyNames, FeatureOtherCatalogNames)
		if err != nil || material {
			t.Fatalf("empty name values warned: entries=%v material=%v err=%v", entries, material, err)
		}
	}
	if material, err := observer.treeValue(t.Context(), types.Array{types.StringLiteral("unpaired")}, keyNames, FeatureOtherCatalogNames); !errors.Is(err, errUnpairedNameEntry) || material {
		t.Fatalf("odd name array accepted: material=%v err=%v", material, err)
	}
	for _, entries := range []types.Array{
		{types.StringLiteral("first"), types.StringLiteral("material"), types.StringLiteral("second"), types.StringLiteral("")},
		{types.StringLiteral("first"), types.StringLiteral(""), types.StringLiteral("second"), types.StringLiteral("material")},
	} {
		if material, err := observer.treeValue(t.Context(), entries, keyNames, FeatureOtherCatalogNames); err != nil || !material {
			t.Fatalf("material name value lost at one pair position: entries=%v material=%v err=%v", entries, material, err)
		}
	}
}

func TestFeatureStreamMaterialityDistinguishesStoredPayload(t *testing.T) {
	t.Parallel()
	zero, one := int64(0), int64(1)
	for _, tc := range []struct {
		name   string
		value  types.StreamDict
		active bool
	}{
		{"no stream data", types.StreamDict{StreamLength: &zero}, false},
		{"declared byte", types.StreamDict{StreamLength: &one}, true},
		{"raw byte", types.StreamDict{StreamLength: &zero, Raw: []byte("x")}, true},
		{"decoded byte", types.StreamDict{Content: []byte("x")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := materialFeatureValue(tc.value); got != tc.active {
				t.Fatalf("stored payload materiality=%v, want %v", got, tc.active)
			}
		})
	}
}

func TestActionMaterialityFollowsPageAndCatalogPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kind   FeatureKind
		action types.Dict
		want   bool
	}{
		{"page local target", FeaturePageActions, types.Dict{"S": types.Name("GoTo")}, false},
		{"catalog local target", FeatureCatalogActions, types.Dict{"S": types.Name("GoTo")}, true},
		{"page external URI", FeaturePageActions, types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral("https://example.invalid")}, false},
		{"catalog external URI", FeatureCatalogActions, types.Dict{"S": types.Name(keyURI), keyURI: types.StringLiteral("https://example.invalid")}, true},
		{"page script", FeaturePageActions, types.Dict{"S": types.Name("JavaScript"), "JS": types.StringLiteral("app.alert('x')")}, true},
		{"empty catalog script", FeatureCatalogActions, types.Dict{"S": types.Name("JavaScript"), "JS": types.StringLiteral("")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := featureTestObserver(t)
			got, err := observer.materialAction(t.Context(), tc.action, tc.kind)
			if err != nil || got != tc.want {
				t.Fatalf("action materiality=%v, want %v: %v", got, tc.want, err)
			}
		})
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
				_, err = observer.materialAction(t.Context(), types.Dict{"S": types.Name(featureJavaScript), "JS": bad}, FeaturePageActions)
			case featurePayloadLabel:
				_, err = observer.attachmentPayload(t.Context(), types.Dict{"EF": types.Dict{"F": bad}})
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

func TestMaterialFeatureTreeAcceptsInclusiveDepthBoundary(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	leaf := types.Dict{keyNames: types.Array{types.StringLiteral("item"), types.StringLiteral("material value")}}
	tree := leaf
	for range maxFeatureDepth {
		tree = types.Dict{"Kids": types.Array{tree}}
	}

	material, err := observer.materialTree(t.Context(), tree, keyNames, FeatureOtherCatalogNames, map[types.IndirectRef]bool{}, 0)
	if err != nil || !material {
		t.Fatalf("material name tree at inclusive depth %d: material=%t err=%v", maxFeatureDepth, material, err)
	}

	tooDeep := types.Dict{"Kids": types.Array{tree}}
	if material, err := observer.materialTree(t.Context(), tooDeep, keyNames, FeatureOtherCatalogNames, map[types.IndirectRef]bool{}, 0); !errors.Is(err, errTreeTooDeep) || material {
		t.Fatalf("name tree beyond depth %d: material=%t err=%v", maxFeatureDepth, material, err)
	}
}

func TestMaterialActionChainAcceptsInclusiveTerminalDepth(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	var action types.Object = types.Dict{"S": types.Name(featureJavaScript), "JS": types.StringLiteral("app.alert('test')")}
	for range maxFeatureDepth - 1 {
		action = types.Array{action}
	}
	material, err := observer.action(t.Context(), action, FeaturePageActions, map[types.IndirectRef]bool{}, 0)
	if err != nil || !material {
		t.Fatalf("material action at inclusive terminal depth %d: material=%t err=%v", maxFeatureDepth, material, err)
	}

	tooDeep := types.Array{action}
	if material, err := observer.action(t.Context(), tooDeep, FeaturePageActions, map[types.IndirectRef]bool{}, 0); !errors.Is(err, errTreeTooDeep) || material {
		t.Fatalf("material action beyond terminal depth %d: material=%t err=%v", maxFeatureDepth, material, err)
	}
}

func TestEmptyCatalogDestinationDoesNotInventAction(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	observer.pdf.RootDict["OpenAction"] = types.Array{}
	facts, err := observeFeatures(t.Context(), observer.pdf)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("empty catalog destination invented source features: %v", facts)
	}
}

func TestCatalogNameTreeFactsSurviveEmptySiblingTrees(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	names := types.Dict{
		"ZMaterial": types.Dict{keyNames: types.Array{types.StringLiteral("entry"), types.StringLiteral("content")}},
	}
	for index := range 32 {
		names[fmt.Sprintf("AEmpty%02d", index)] = types.Dict{}
	}
	observer.pdf.RootDict[keyNames] = names

	for attempt := range 32 {
		facts, err := observeFeatures(t.Context(), observer.pdf)
		if err != nil {
			t.Fatal(err)
		}
		if len(facts) != 1 || facts[0] != (SourceFeature{Kind: FeatureOtherCatalogNames, Disposition: FeatureRemoved}) {
			t.Fatalf("name-tree scan %d lost material sibling: %v", attempt, facts)
		}
	}
}

func TestNonAttachmentAnnotationDoesNotClaimEmbeddedPayload(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	attachmentLike := types.Dict{
		keySubtype: types.Name("Link"),
		"FS":       types.Dict{"EF": types.Dict{"F": types.StreamDict{Raw: []byte("payload")}}},
	}
	if retained, err := observer.attachmentPayload(t.Context(), attachmentLike["FS"]); err != nil || !retained {
		t.Fatalf("embedded-file precondition: retained=%t err=%v", retained, err)
	}
	if err := observer.annotation(t.Context(), attachmentLike); err != nil {
		t.Fatal(err)
	}
	if observer.found[FeaturePageAttachments] {
		t.Fatal("non-attachment annotation claimed a retained embedded file")
	}
	if err := observer.annotation(t.Context(), types.Dict{}); err != nil {
		t.Fatalf("annotation without subtype: %v", err)
	}
	if observer.found[FeaturePageAttachments] {
		t.Fatal("non-attachment annotation claimed a retained embedded file")
	}
}

func TestFieldActionsAcceptInclusiveTerminalDepth(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)
	field := types.Dict{"A": types.Dict{"S": types.Name(featureJavaScript), "JS": types.StringLiteral("app.alert('field')")}}
	for range maxFeatureDepth - 1 {
		field = types.Dict{"Kids": types.Array{field}}
	}
	if err := observer.fieldActions(t.Context(), types.Array{field}, map[types.IndirectRef]bool{}, 0); err != nil || !observer.found[FeaturePageActions] {
		t.Fatalf("field action at inclusive terminal depth %d: found=%t err=%v", maxFeatureDepth, observer.found[FeaturePageActions], err)
	}

	tooDeep := types.Dict{"Kids": types.Array{field}}
	if err := observer.fieldActions(t.Context(), types.Array{tooDeep}, map[types.IndirectRef]bool{}, 0); !errors.Is(err, errTreeTooDeep) {
		t.Fatalf("field actions beyond terminal depth %d: %v", maxFeatureDepth, err)
	}
}

func TestMalformedFeatureContainersRejectWithoutInventingObservations(t *testing.T) {
	t.Parallel()

	for _, dict := range []types.Dict{
		{keyNames: types.Dict{"OtherTree": types.Integer(7)}},
		{keyAcroForm: types.Integer(7)},
		{keyAcroForm: types.Dict{keyFields: types.Integer(7)}},
		{keyAcroForm: types.Dict{keyFields: types.Array{types.Integer(7)}}},
		{"PageLabels": types.Dict{keyKids: types.Integer(7)}},
		{"PageLabels": types.Dict{keyKids: types.Array{types.Integer(7)}}},
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

	if material, err := observer.attachmentPayload(t.Context(), types.Dict{}); err != nil || material {
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

	for _, field := range []types.Dict{{"AA": types.Integer(7)}, {keyKids: types.Integer(7)}} {
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
		if material, err := materialScript(t.Context(), object); err == nil {
			t.Fatalf("undecodable script treated as material=%v", material)
		}
	}

	if material, err := materialScript(t.Context(), types.Integer(7)); err != nil || material {
		t.Fatalf("non-script object became active content: %v %v", material, err)
	}
	for _, tc := range []struct {
		value types.Object
		want  bool
	}{
		{types.HexLiteral(""), false},
		{types.HexLiteral("61"), true},
		{types.StringLiteral(""), false},
		{types.StringLiteral("a"), true},
	} {
		if material, err := materialScript(t.Context(), tc.value); err != nil || material != tc.want {
			t.Fatalf("script payload %v: material=%v error=%v", tc.value, material, err)
		}
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

	field[keyKids] = types.Array{reference}

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
