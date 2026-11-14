// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestStructureFittingGate(t *testing.T) {
	var dest *model.Context
	var parts []structurePart
	for range 2 {
		s, _, err := readSource(t.Context(), pdffixture.Tagged("retained").Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if err = pruneStructure(t.Context(), s); err != nil {
			t.Fatal(err)
		}
		pages, err := pageGeometries(t.Context(), s)
		if err != nil {
			t.Fatal(err)
		}
		// Exercise the same Form creation used by fitting, then relocate ownership.
		original := pages[0]
		if err = fitContext(t.Context(), s, &packet.Size{Width: 300, Height: 400}); err != nil {
			t.Fatal(err)
		}
		attrs, err := s.DereferenceDict(original.dict["Resources"])
		if err != nil {
			t.Fatal(err)
		}
		forms, err := s.DereferenceDict(attrs["XObject"])
		if err != nil {
			t.Fatal(err)
		}
		form, ok := forms["PCSource"].(types.IndirectRef)
		if !ok {
			t.Fatal("fitting did not create a Form")
		}
		m, _ := fitMatrix(original.box, original.attrs.Rotate, &packet.Size{Width: 300, Height: 400})
		if err = fitStructure(t.Context(), s, original.ref, form, m); err != nil {
			t.Fatal(err)
		}
		root, err := structureRoot(s)
		if err != nil {
			t.Fatal(err)
		}
		cat, err := s.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, structurePart{root: root, language: cat["Lang"]})
		if dest == nil {
			dest = s
		} else if err = pdfcpu.MergeXRefTables(t.Context(), "", s, dest, false, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := combineStructure(t.Context(), dest, parts); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := serialize(t.Context(), dest, &out); err != nil {
		t.Fatal(err)
	}
	s, _, err := readSource(t.Context(), out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	root, err := structureRoot(s)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.DereferenceDict(root["ParentTree"])
	if err != nil {
		t.Fatal(err)
	}
	nums, err := s.DereferenceArray(parent["Nums"])
	if err != nil {
		t.Fatal(err)
	}
	if len(nums) != 4 {
		t.Fatalf("wanted two independently owned Forms, got %v", nums)
	}
	for i := 1; i < len(nums); i += 2 {
		a, err := s.DereferenceArray(nums[i])
		if err != nil || len(a) != 1 {
			t.Fatalf("invalid form parent array: %v %v", a, err)
		}
	}
}

func TestNestedTaggedFormsRetainStreamOwnership(t *testing.T) {
	source := pdffixture.Tagged("nested")
	source.Objs[2] = bytes.Replace(source.Objs[2], []byte("/StructParents 0"), nil, 1)
	source.Objs[2] = bytes.Replace(source.Objs[2], []byte("/Resources <<"), []byte("/Resources << /XObject << /Nested 9 0 R >>"), 1)
	content := []byte("q /Nested Do Q")
	source.Objs[3] = []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	source.Objs[6] = bytes.Replace(source.Objs[6], []byte("/K 0"), []byte("/K << /Type /MCR /Pg 3 0 R /Stm 9 0 R /MCID 0 >>"), 1)
	nested := []byte("/P <</MCID 0>> BDC BT /F1 24 Tf 50 50 Td (nested) Tj ET EMC")
	source.Objs = append(source.Objs, []byte(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /StructParents 0 /Length %d >>\nstream\n%s\nendstream", len(nested), nested)))
	s, _, err := readSource(t.Context(), source.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = pruneStructure(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	original, err := structureRoot(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = fitContext(t.Context(), s, &packet.Size{Width: 300, Height: 400}); err != nil {
		t.Fatal(err)
	}
	if err = combineStructure(t.Context(), s, []structurePart{{root: original}}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = serialize(t.Context(), s, &out); err != nil {
		t.Fatal(err)
	}
	reopened, _, err := readSource(t.Context(), out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	root, err := structureRoot(reopened)
	if err != nil {
		t.Fatal(err)
	}
	ownership, err := numberTree(t.Context(), reopened, root["ParentTree"])
	if err != nil {
		t.Fatal(err)
	}
	if len(ownership) != 1 {
		t.Fatalf("outer fitting Form acquired false semantic ownership: %v", ownership)
	}
}
