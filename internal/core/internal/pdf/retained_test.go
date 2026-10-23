// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestRetainedTraversalRolesAndCycles(t *testing.T) {
	s, err := read(t.Context(), pdffixture.Plain("traversal").Bytes())
	if err != nil {
		t.Fatal(err)
	}
	cat, err := s.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	widget := types.Dict{"Subtype": types.Name("Widget"), "A": types.Dict{"S": types.Name("GoTo"), "D": types.Array{types.Integer(0), types.Name("Fit")}}}
	ref, err := s.IndRefForNewObject(widget)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := rawPages(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	pages[0].dict["Annots"] = types.Array{*ref}
	cat["AcroForm"] = types.Dict{"Fields": types.Array{*ref}}
	annotations, targets := 0, 0
	err = retainedObjects(t.Context(), s, func(types.Dict) error { annotations++; return nil }, func(o types.Object) (bool, error) {
		if o != nil {
			targets++
		}
		return true, nil
	})
	if err != nil || annotations != 1 || targets != 1 {
		t.Fatalf("Widget visited repeatedly: annotations=%d targets=%d err=%v", annotations, targets, err)
	}
	delete(cat, "AcroForm")
	delete(pages[0].dict, "Annots")
	outline := types.Dict{"Dest": types.Array{types.Integer(0), types.Name("Fit")}}
	ref, err = s.IndRefForNewObject(outline)
	if err != nil {
		t.Fatal(err)
	}
	outline["Next"] = *ref
	cat["Outlines"] = types.Dict{"First": *ref}
	targets = 0
	err = retainedObjects(t.Context(), s, func(types.Dict) error { return nil }, func(o types.Object) (bool, error) {
		if o != nil {
			targets++
		}
		return true, nil
	})
	if err != nil || targets != 1 {
		t.Fatalf("sibling cycle did not terminate: targets=%d err=%v", targets, err)
	}
}
