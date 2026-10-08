// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

const (
	omittedPolicyWord = "omits"
	reviewPolicyWord  = "review"
)

func TestSourceFeatureAdviceNamesConcretePolicyAndScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind pdfengine.FeatureKind
		need []string
	}{
		{pdfengine.FeatureBookmarks, []string{"bookmarks", omittedPolicyWord, reviewPolicyWord}},
		{pdfengine.FeatureTaggedStructure, []string{"tags", "accessible", "workflow"}},
		{pdfengine.FeatureCatalogAttachments, []string{"index", "payloads", "can remain"}},
		{pdfengine.FeatureCatalogActions, []string{"Catalog actions", omittedPolicyWord, "separate scope"}},
		{pdfengine.FeaturePageActions, []string{"retains", "without executing", "qualified", reviewPolicyWord}},
		{pdfengine.FeaturePageAttachments, []string{"retains", "payloads", "does not sanitize"}},
		{pdfengine.FeaturePageLabels, []string{omittedPolicyWord, "page references"}},
		{pdfengine.FeatureOtherCatalogNames, []string{"only destination", reviewPolicyWord}},
		{pdfengine.FeatureKind("unexpected_backend_fact"), []string{"unexpected_backend_fact", "retained", reviewPolicyWord}},
	}
	for _, test := range cases {
		message := sourceFeatureMessage(pdfengine.SourceFeature{Kind: test.kind, Disposition: pdfengine.FeatureRetained})
		for _, fact := range test.need {
			if !strings.Contains(message, fact) {
				t.Fatalf("policy %q omits repair/scope fact %q: %s", test.kind, fact, message)
			}
		}
	}
}
