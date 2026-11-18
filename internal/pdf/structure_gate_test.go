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
	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestStructureFittingGate(t *testing.T) {
	var dest *model.Context
	var parts []structurePart
	for range 2 {
		s, _, _, err := readSource(t.Context(), pdffixture.Tagged("retained").Bytes(), "")
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
		if err = fitToForTest(t.Context(), s, packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400})); err != nil {
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
	s, _, _, err := readSource(t.Context(), out.Bytes(), "")
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
	s, _, _, err := readSource(t.Context(), source.Bytes(), "")
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
	if err = fitToForTest(t.Context(), s, packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400})); err != nil {
		t.Fatal(err)
	}
	if err = combineStructure(t.Context(), s, []structurePart{{root: original}}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = serialize(t.Context(), s, &out); err != nil {
		t.Fatal(err)
	}
	reopened, _, _, err := readSource(t.Context(), out.Bytes(), "")
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

func TestLeadingSourceRetainsInheritedAndExplicitLanguage(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		input := pdffixture.Tagged("language")
		lang := "(lv-LV)"
		if indirect {
			lang = "10 0 R"
		}
		input.Objs[0] = bytes.Replace(input.Objs[0], []byte("/Lang (en)"), []byte("/Lang "+lang), 1)
		program := "/P <</MCID 0>> BDC BT /F1 12 Tf 40 700 Td (Inherited) Tj ET EMC /Span <</MCID 1>> BDC BT /F1 12 Tf 40 680 Td (Explicit) Tj ET EMC"
		input.Objs[3] = []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program))
		input.Objs[6] = bytes.Replace(input.Objs[6], []byte("/K 0"), []byte("/K [0 9 0 R]"), 1)
		input.Objs[7] = []byte("<< /Nums [0 [7 0 R 9 0 R]] >>")
		input.Objs = append(input.Objs, []byte("<< /Type /StructElem /S /Span /P 7 0 R /Pg 3 0 R /K 1 /Lang (lt-LT) >>"))
		if indirect {
			input.Objs = append(input.Objs, []byte("(lv-LV)"))
		}
		data := input.Bytes()
		facts, slots, findings := Inspect(t.Context(), data, "", []SelectionRequest{{Item: "source"}})
		if len(findings) > 0 || len(slots) != 1 || slots[0] == nil {
			t.Fatal(findings)
		}
		for _, generatedFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("indirect=%v/generated-first=%v", indirect, generatedFirst), func(t *testing.T) {
				var parts []Part
				size := packet.Size{Width: 612, Height: 792}
				if generatedFirst {
					parts = append(parts, Part{Part: packet.Part{Leaf: packet.Leaf{Kind: packet.PartPage, Ref: "cover"},
						Pages: 1, FirstPage: 1, LastPage: 1, Size: size, OutputSize: size},
						Generated: []compose.Page{{Width: size.Width, Height: size.Height}}, Repeat: 1})
				}
				page := int64(len(parts) + 1)
				parts = append(parts, Part{Part: packet.Part{Leaf: packet.Leaf{Kind: packet.PartSource, Ref: "source", Source: "source.pdf"},
					Pages: 1, FirstPage: page, LastPage: page}, Input: pdffixture.SourceInput(data), Occurrence: &Occurrence{File: &facts, Facts: slots[0]}})
				authored := "ja-JP"
				var out bytes.Buffer
				if _, err := Assemble(t.Context(), Assembly{Parts: parts, Metadata: &packet.Metadata{Language: &authored}}, &out); err != nil {
					t.Fatal(err)
				}
				doc, err := readOutput(t.Context(), out.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				catalog, err := doc.Catalog()
				if err != nil {
					t.Fatal(err)
				}
				if lang, err := textValue(doc, catalog["Lang"]); err != nil || lang != authored {
					t.Fatalf("output catalog language is %q: %v", lang, err)
				}
				root, err := structureRoot(doc)
				if err != nil {
					t.Fatal(err)
				}
				tree, err := numberTree(t.Context(), doc, root["ParentTree"])
				if err != nil {
					t.Fatal(err)
				}
				pages, err := rawPages(t.Context(), doc)
				if err != nil || int(page) > len(pages) {
					t.Fatalf("missing source output page %d: %v", page, err)
				}
				index, err := doc.DereferenceInteger(pages[page-1].dict["StructParents"])
				if err != nil || index == nil {
					t.Fatalf("missing marked-content owner: %v", err)
				}
				parents, err := doc.DereferenceArray(tree[index.Value()])
				if err != nil || len(parents) != 2 {
					t.Fatalf("invalid page ownership: %v %v", parents, err)
				}
				for i, want := range []string{"lv-LV", "lt-LT"} {
					elem, err := doc.DereferenceDict(parents[i])
					if err != nil {
						t.Fatal(err)
					}
					var inherited string
					for depth := 0; depth < 8 && elem != nil; depth++ {
						inherited, err = textValue(doc, elem["Lang"])
						if err != nil {
							t.Fatal(err)
						}
						if inherited != "" {
							break
						}
						elem, err = doc.DereferenceDict(elem["P"])
						if err != nil {
							t.Fatal(err)
						}
					}
					if inherited != want {
						t.Fatalf("MCID %d inherited %q, want %q", i, inherited, want)
					}
				}
			})
		}
	}
}
