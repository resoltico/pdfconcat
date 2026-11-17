// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

func TestPartFactBindingsRefuseInconsistency(t *testing.T) {
	part := pdf.Part{Part: packet.Part{Leaf: packet.Leaf{Ref: "page", Kind: packet.PartPage}, FirstPage: 1}}
	for _, facts := range [][]compose.PreparedPart{nil, {{Owner: packet.PartIdentity{Ref: "other", Kind: packet.PartPage, FirstPage: 1}}}} {
		parts := []pdf.Part{part}
		if ds := bindComposition(parts, facts, true); len(ds) != 1 || diag.Outcome(ds) != diag.StatusFailed || parts[0].Text != nil {
			t.Fatalf("misaligned composition became plausible: %+v", ds)
		}
	}
	parts := []pdf.Part{part}
	prepared := []compose.PreparedPart{{Owner: part.Identity(), Pages: []compose.Page{{Width: 100, Height: 200}}, Text: &compose.TextFacts{Font: "expected"}}}
	if ds := bindComposition(parts, prepared, true); len(ds) > 0 || parts[0].Text.Font != "expected" {
		t.Fatal(ds)
	}
	for _, facts := range []pdf.Assembled{{}, {Parts: map[packet.PartIdentity]pdf.PartResult{part.Identity(): {Known: false}}}, {Parts: map[packet.PartIdentity]pdf.PartResult{{Ref: "other"}: {Known: true}}}} {
		if ds := bindAssemblyFacts(parts, facts, true); len(ds) != 1 || diag.Outcome(ds) != diag.StatusFailed {
			t.Fatalf("misaligned actual facts became plausible: %+v", ds)
		}
	}
	if ds := bindAssemblyFacts(parts, pdf.Assembled{}, false); len(ds) > 0 || parts[0].FieldsRenamed != nil {
		t.Fatal("partial unknown facts were fabricated", ds)
	}
}
