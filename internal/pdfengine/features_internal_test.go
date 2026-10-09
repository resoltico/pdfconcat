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
	featureJavaScript = "JavaScript"
	featureScript     = "true"
)

func TestSourceFeaturesObserveRealCatalogFactsAndSilentOrdinarySources(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		doc  *pdffixture.Doc
		kind FeatureKind
	}{
		{pdffixture.Plain("ordinary"), ""},
		{pdffixture.Links("links"), ""},
		{pdffixture.Form("form"), ""},
		{pdffixture.PageActions("ordinary GoTo"), ""},
		{pdffixture.Outlined("bookmarks"), FeatureBookmarks},
		{pdffixture.Tagged("tags"), FeatureTaggedStructure},
		{pdffixture.Attachment("payload"), FeatureCatalogAttachments},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			t.Parallel()
			pdf := readUnvalidated(t, test.doc)

			features, err := observeFeatures(t.Context(), pdf)
			if err != nil {
				t.Fatal(err)
			}

			var want []SourceFeature
			if test.kind != "" {
				want = []SourceFeature{{Kind: test.kind, Disposition: FeatureRemoved}}
			}

			if !reflect.DeepEqual(features, want) {
				t.Fatalf("features %v want %v", features, want)
			}
		})
	}
}

func TestSourceFeaturesDistinguishCatalogIndicesFromRetainedPayloadAndActions(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, pdffixture.Attachment("payload"))

	root, err := pdf.Pages()
	if err != nil {
		t.Fatal(err)
	}

	err = walkPages(t.Context(), pdf, *root, func(_ types.IndirectRef, page types.Dict, _ inheritedAttrs) error {
		page["AA"] = types.Dict{"O": types.Dict{"S": types.Name(featureJavaScript), "JS": types.StringLiteral("app.alert('test')")}}

		names, readErr := pdf.DereferenceDict(pdf.RootDict[keyNames])
		if readErr != nil {
			return fmt.Errorf("fixture names: %w", readErr)
		}

		embedded, readErr := pdf.DereferenceDict(names["EmbeddedFiles"])
		if readErr != nil {
			return fmt.Errorf("fixture attachments: %w", readErr)
		}

		objects, readErr := pdf.DereferenceArray(embedded[keyNames])
		if readErr != nil {
			return fmt.Errorf("fixture names entries: %w", readErr)
		}

		page["Annots"] = types.Array{types.Dict{keySubtype: types.Name("FileAttachment"), "FS": objects[1]}}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	pdf.RootDict["OpenAction"] = types.Dict{"S": types.Name(featureJavaScript), "JS": types.StringLiteral("app.alert('catalog')")}

	features, err := observeFeatures(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}

	want := []SourceFeature{
		{Kind: FeatureCatalogAttachments, Disposition: FeatureRemoved},
		{Kind: FeatureCatalogActions, Disposition: FeatureRemoved},
		{Kind: FeaturePageActions, Disposition: FeatureRetained},
		{Kind: FeaturePageAttachments, Disposition: FeatureRetained},
	}
	if !reflect.DeepEqual(features, want) {
		t.Fatalf("scope facts %v", features)
	}
}

func TestFeatureEmptyStateAndTextDecoysAreSilent(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, pdffixture.Plain("JavaScript EmbeddedFiles /Sig /ByteRange text decoys"))
	pdf.RootDict[keyNames] = types.Dict{
		"EmbeddedFiles": types.Dict{keyNames: types.Array{}},
		featureJavaScript: types.Dict{
			keyNames: types.Array{
				types.StringLiteral("empty"),
				types.Dict{"S": types.Name(featureJavaScript), "JS": types.StringLiteral("")},
			},
		},
	}
	pdf.RootDict["Outlines"] = types.Dict{}
	pdf.RootDict["StructTreeRoot"] = types.Dict{}
	pdf.RootDict["PageLabels"] = types.Dict{"Nums": types.Array{}}

	features, err := observeFeatures(t.Context(), pdf)
	if err != nil || len(features) != 0 {
		t.Fatalf("empty/decoy facts %v, %v", features, err)
	}
}

func TestFeatureTraversalRejectsCyclesDepthAndCancellation(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, pdffixture.Plain("safe"))
	observer := featureObserver{pdf: pdf, found: map[FeatureKind]bool{}}
	ref := *types.NewIndirectRef(99, 0)
	dict := types.Dict{"S": types.Name(featureJavaScript), "JS": types.StringLiteral(featureScript), keyNext: ref}
	pdf.Table[99] = model.NewXRefTableEntryGen0(dict)

	if _, err := observer.action(t.Context(), ref, FeaturePageActions, map[types.IndirectRef]bool{}, 0); !errors.Is(err, errNodeRepeated) {
		t.Fatalf("action cycle %v", err)
	}

	dict = types.Dict{keyKids: types.Array{ref}}

	pdf.Table[99] = model.NewXRefTableEntryGen0(dict)

	_, nameErr := observer.materialTree(
		t.Context(),
		dict,
		keyNames,
		FeatureOtherCatalogNames,
		map[types.IndirectRef]bool{},
		0,
	)
	if !errors.Is(nameErr, errNodeRepeated) {
		t.Fatalf("name cycle %v", nameErr)
	}

	_, depthErr := observer.action(
		t.Context(),
		nil,
		FeaturePageActions,
		map[types.IndirectRef]bool{},
		maxFeatureDepth+1,
	)
	if !errors.Is(depthErr, errTreeTooDeep) {
		t.Fatalf("depth %v", depthErr)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := observeFeatures(ctx, pdf); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}
}

func TestAssociatedFileIndexAndIndirectActionNamesAreObserved(t *testing.T) {
	t.Parallel()
	pdf := readUnvalidated(t, pdffixture.Attachment("associated"))

	names, err := pdf.DereferenceDict(pdf.RootDict[keyNames])
	if err != nil {
		t.Fatal(err)
	}

	embedded, err := pdf.DereferenceDict(names["EmbeddedFiles"])
	if err != nil {
		t.Fatal(err)
	}

	entries, err := pdf.DereferenceArray(embedded[keyNames])
	if err != nil {
		t.Fatal(err)
	}

	pdf.RootDict.Delete(keyNames)
	pdf.RootDict["AF"] = types.Array{entries[1]}
	ref := *types.NewIndirectRef(99, 0)
	pdf.Table[99] = model.NewXRefTableEntryGen0(types.Name(featureJavaScript))
	pdf.RootDict["OpenAction"] = types.Dict{"S": ref, "JS": types.StringLiteral(featureScript)}

	features, err := observeFeatures(t.Context(), pdf)
	if err != nil {
		t.Fatal(err)
	}

	want := []SourceFeature{
		{Kind: FeatureCatalogAttachments, Disposition: FeatureRemoved},
		{Kind: FeatureCatalogActions, Disposition: FeatureRemoved},
	}
	if !reflect.DeepEqual(features, want) {
		t.Fatalf("associated/indirect facts %v", features)
	}
}

func TestEmptyCompressedJavaScriptStreamDoesNotWarn(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"", featureScript} {
		stream := types.StreamDict{Dict: types.Dict{}, Content: []byte(content), FilterPipeline: []types.PDFFilter{{Name: "FlateDecode"}}}
		if err := stream.Encode(); err != nil {
			t.Fatal(err)
		}

		stream.Content = nil

		material, err := materialScript(t.Context(), stream)
		if err != nil || material != (content != "") {
			t.Fatalf("script %q: material=%v error=%v", content, material, err)
		}
	}
}

func TestMalformedFeatureReferencesAndNameEntriesReject(t *testing.T) {
	t.Parallel()

	for _, dict := range []types.Dict{
		{"Outlines": types.Integer(7)},
		{keyNames: types.Integer(7)},
		{keyNames: types.Dict{"EmbeddedFiles": types.Dict{keyNames: types.Array{types.StringLiteral("unpaired")}}}},
		{"AF": types.Integer(7)},
		{"OpenAction": types.Dict{"S": types.Integer(7)}},
	} {
		pdf := readUnvalidated(t, pdffixture.Plain("malformed feature"))
		maps.Copy(pdf.RootDict, dict)

		if features, err := observeFeatures(t.Context(), pdf); err == nil {
			t.Fatalf("malformed features approved: %v, %v", dict, features)
		}
	}
}
