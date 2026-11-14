// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestUnusedMalformedClassMapHasActionableProvenance(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		d := pdffixture.Tagged("CLASSMAP")
		declaration := "<< /Type /StructTreeRoot >>"
		object := 6
		if indirect {
			d.Objs = append(d.Objs, []byte(declaration))
			declaration = "9 0 R"
			object = 9
		}
		d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/Type /StructTreeRoot"), []byte("/Type /StructTreeRoot /ClassMap "+declaration), 1)
		_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
		status, _ := diag.Outcome(ds)
		if status != "invalid" || len(ds) != 1 {
			t.Fatal(ds)
		}
		f := ds[0]
		if f.PDFLocation == nil || f.PDFLocation.Object != object || !strings.Contains(f.PDFLocation.Path, "ClassMap") || !strings.Contains(f.Message, "discard") {
			t.Fatalf("class map attribution: %+v", f)
		}
		_, _, ds = Inspect(t.Context(), d.Bytes(), packet.StructureDiscard, []SelectionRequest{{Item: "source"}})
		if status, _ = diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
	}
}
