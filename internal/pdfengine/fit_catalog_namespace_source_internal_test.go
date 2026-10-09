// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

const fitCatalogSourceVersion = "1.7"

func TestFitCatalogSourceRefusalPrioritizesLegacyNamespaceAndLexicalName(t *testing.T) {
	t.Parallel()

	var legacy, tree strings.Builder
	for index := range 64 {
		fmt.Fprintf(&legacy, "/chapter%02d [3 0 R /XYZ 0 0 1] ", index)
		fmt.Fprintf(&tree, "(chapter%02d) [3 0 R /XYZ 0 0 1] ", index)
	}

	doc := &pdffixture.Doc{Version: fitCatalogSourceVersion, Objs: [][]byte{
		fmt.Appendf(
			nil, "<< /Type /Catalog /Pages 2 0 R /Dests << %s >> /Names << /Dests << /Names [%s] >> >> >>",
			legacy.String(), tree.String(),
		),
		[]byte("<< /Type /Pages /Count 1 /Kids [3 0 R] /MediaBox [0 0 100 100] >>"),
		[]byte("<< /Type /Page /Parent 2 0 R >>"),
	}}
	engine, path := fitBoundaryDocument(t, doc)

	if info, err := engine.Inspect(t.Context(), path, nil); err != nil || info.Pages != 1 {
		t.Fatalf("valid native coordinate destinations rejected without fitting: %v", err)
	}

	for _, target := range []PageSize{{595, 842}, {612, 1008}} {
		_, err := engine.Inspect(t.Context(), path, &target)
		if err == nil || !strings.Contains(err.Error(), `named destination "chapter00" (legacy=true)`) {
			t.Fatalf("typed destination refusal lost stable namespace/lexical attribution: %v", err)
		}
	}
}
