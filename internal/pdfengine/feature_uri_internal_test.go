// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"reflect"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const (
	uriAnnex      = "annex.pdf"
	uriReportBase = "https://example.invalid/report/"
	uriRootBase   = "https://example.invalid/"
)

func TestURIBaseDisclosureUsesDecodedTargetAndActiveBinding(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, target, base string
		isMap, warn        bool
	}{
		{"relative with base", uriAnnex, uriReportBase, false, true},
		{"mapped relative", uriAnnex, uriReportBase, true, true},
		{"fragment", "#section", uriReportBase, false, true},
		{"network relative", "//example.invalid/path", uriRootBase, false, true},
		{"absolute", "https://example.invalid/annex.pdf", uriReportBase, false, false},
		{"mapped absolute", "https://example.invalid/annex.pdf", uriReportBase, true, false},
		{"escaped absolute scheme", "https\\072//example.invalid/annex.pdf", uriRootBase, false, false},
		{"opaque absolute", "mailto:user@example.invalid", uriRootBase, false, false},
		{"no explicit base", uriAnnex, "", false, false},
		{"empty target", "", uriRootBase, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pdf := readUnvalidated(t, pdffixture.URILink("URI binding", test.target, test.base, test.isMap))

			facts, err := observeFeatures(t.Context(), pdf)
			if err != nil {
				t.Fatal(err)
			}

			var want []SourceFeature
			if test.warn {
				want = []SourceFeature{{Kind: FeatureURIBase, Disposition: FeatureRemoved}}
			}

			if !reflect.DeepEqual(facts, want) {
				t.Fatalf("binding facts %v, want %v", facts, want)
			}
		})
	}
}

func TestURIBaseTraversesReferencedSequencesWithoutDuplicateEffects(t *testing.T) {
	t.Parallel()
	observer := featureTestObserver(t)

	observer.pdf.RootDict[keyURI] = types.Dict{keyURIBase: types.HexLiteral("68747470733a2f2f6578616d706c652e696e76616c69642f")}
	if err := observer.catalog(t.Context()); err != nil {
		t.Fatal(err)
	}

	target := signatureTestObject(observer.pdf, types.HexLiteral("616e6e65782e706466"))
	action := signatureTestObject(observer.pdf, types.Dict{"S": types.Name(keyURI), keyURI: target})

	sequence := types.Dict{
		"S": types.Name(keyURI), keyURI: types.StringLiteral("https://example.invalid"),
		keyNext: types.Array{action, action},
	}
	if err := observer.actions(t.Context(), types.Dict{"A": sequence}, FeaturePageActions); err != nil {
		t.Fatal(err)
	}

	want := []SourceFeature{{Kind: FeatureURIBase, Disposition: FeatureRemoved}}
	if facts := observer.result(); !reflect.DeepEqual(facts, want) {
		t.Fatalf("referenced binding facts: %v", facts)
	}

	for _, target := range []types.Object{types.Integer(7), types.HexLiteral("zz"), malformedLazyObject(observer.pdf)} {
		_, actionErr := observer.materialAction(
			t.Context(),
			types.Dict{"S": types.Name(keyURI), keyURI: target},
			FeaturePageActions,
		)
		if actionErr == nil {
			t.Fatalf("malformed URI target accepted: %v", target)
		}
	}
}

func TestURIBaseEmptyAndMalformedContainers(t *testing.T) {
	t.Parallel()

	for _, container := range []types.Object{nil, types.Dict{}, types.Dict{keyURIBase: types.StringLiteral("")}} {
		pdf := readUnvalidated(t, pdffixture.URILink("empty base", uriAnnex, "", false))
		pdf.RootDict["URI"] = container

		facts, err := observeFeatures(t.Context(), pdf)
		if err != nil || len(facts) != 0 {
			t.Fatalf("empty base facts %v: %v", facts, err)
		}
	}

	for _, container := range []types.Object{types.Integer(7), types.Dict{keyURIBase: types.Integer(7)}} {
		pdf := readUnvalidated(t, pdffixture.Plain("malformed base"))
		pdf.RootDict["URI"] = container

		if _, err := observeFeatures(t.Context(), pdf); err == nil {
			t.Fatalf("malformed URI base accepted: %v", container)
		}
	}
}
