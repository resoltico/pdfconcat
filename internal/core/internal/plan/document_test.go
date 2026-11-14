// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/plan"
)

func TestDocumentAndNumberingGrammar(t *testing.T) {
	for _, settings := range []string{
		`"metadata":{"title":"Rīga <&> Ελληνικά","author":"One; authored string","language":"x-private"},"timestamp":0`,
		`"metadata":{"title":"","language":"xyz"},"timestamp":253402300799`,
		`"page_numbers":{"mode":"consecutive","start":9,"kinds":[],"position":"top-left"},"structure":"discard"`,
	} {
		_, ds := plan.Decode([]byte(`{"version":5,`+settings+`,"items":[{"page":{"size":"A4"},"numbering":false}]}`), "plan.json", t.TempDir())
		if len(ds) > 0 {
			t.Fatalf("%s: %v", settings, ds)
		}
	}
	for _, settings := range []string{
		`"metadata":{"language":"en-a-foo-a-bar"}`, `"metadata":{"language":"en-abcde-abcde"}`, `"metadata":{"language":"en-Latn-Latn"}`, `"metadata":{"language":"en_US"}`, `"metadata":{"language":"en--US"}`, `"metadata":{"title":"\u0001"}`, `"metadata":{"author":null}`, `"metadata":{"title":"a","title":"b"}`, `"timestamp":-1`, `"timestamp":253402300800`, `"timestamp":null`, `"timestamp":1.5`, `"structure":"strip"`, `"page_numbers":{"start":0}`, `"page_numbers":{"kinds":["padding"]}`, `"page_numbers":{"kinds":["page","page"]}`, `"page_numbers":{"position":"center"}`, `"page_numbers":{"mode":"other"}`,
	} {
		_, ds := plan.Decode([]byte(`{"version":5,`+settings+`,"items":[{"page":{"size":"A4"}}]}`), "plan.json", t.TempDir())
		if len(ds) != 1 {
			t.Fatalf("accepted invalid %s: %v", settings, ds)
		}
	}
}
