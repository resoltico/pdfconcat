// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

// Key 3 is a stale scalar reverse entry for a retained paragraph. No page,
// annotation or Form claims that key; live pages use the three array entries.
// This is authored here rather than importing any producer PDF.
func staleParentIndexFixture(directTree, indirectArrays bool) *pdffixture.Doc {
	d := taggedSelectionFixture()
	nums := "[0 [12 0 R] 1 [13 0 R] 2 [14 0 R] 3 12 0 R]"
	if indirectArrays {
		d.Objs = append(d.Objs, []byte("[12 0 R]"), []byte("[13 0 R]"), []byte("[14 0 R]"))
		nums = "[0 19 0 R 1 20 0 R 2 21 0 R 3 12 0 R]"
		d.Objs = append(d.Objs, []byte(nums))
		nums = "22 0 R"
	}
	tree := "<< /Limits [0 3] /Nums " + nums + " >>"
	d.Objs[10] = []byte(tree)
	if directTree {
		d.Objs[9] = bytes.Replace(d.Objs[9], []byte("/ParentTree 11 0 R"), []byte("/ParentTree "+tree), 1)
	}
	return d
}

func expectIndexNormalization(t *testing.T, d *pdffixture.Doc, path string) {
	t.Helper()
	_, slots, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
	status := diag.Outcome(ds)
	if status != "ok" || slots[0] == nil {
		t.Fatalf("safe source index refused: %+v", ds)
	}
	found := false
	for _, finding := range ds {
		if finding.Code.String() != "source_structure_index_normalized" {
			continue
		}
		found = true
		if finding.PDFLocation == nil || finding.PDFLocation.Object <= 0 || !strings.Contains(finding.PDFLocation.Path, path) {
			t.Fatalf("missing index evidence: %+v", finding)
		}
	}
	if !found {
		t.Fatalf("source normalization was not disclosed: %+v", ds)
	}
}

func TestStaleUnclaimedParentIndexNormalization(t *testing.T) {
	for _, directTree := range []bool{false, true} {
		for _, indirectArrays := range []bool{false, true} {
			t.Run(fmt.Sprintf("direct=%v/indirect-arrays=%v", directTree, indirectArrays), func(t *testing.T) {
				d := staleParentIndexFixture(directTree, indirectArrays)
				expectIndexNormalization(t, d, "ParentTree")
				for _, fit := range []*packet.Size{nil, {Width: 300, Height: 400}} {
					path, ds := selectedPacket(t, d, []packet.Item{
						&packet.SourceItem{Ref: "first", Path: "source"},
						&packet.SourceItem{Ref: "repeated", Path: "source"},
					}, fit)
					if status := diag.Outcome(ds); status != "ok" {
						t.Fatal(ds)
					}
					raw, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					out, err := readOutput(t.Context(), raw)
					if err != nil {
						t.Fatal(err)
					}
					root, err := structureRoot(out)
					if err != nil {
						t.Fatal(err)
					}
					index, err := numberTree(t.Context(), out, root["ParentTree"])
					if err != nil || len(index) != 6 {
						t.Fatalf("six retained page/Form owners: %v %v", index, err)
					}
					for key, value := range index {
						a, err := out.DereferenceArray(value)
						if err != nil || len(a) != 1 {
							t.Fatalf("key %d is not a retained marked-content owner: %v %v", key, a, err)
						}
					}
					ids, err := structureIDTree(t.Context(), out, root["IDTree"])
					if err != nil || len(ids) != 6 {
						t.Fatalf("retained occurrence IDs: %v %v", ids, err)
					}
					// Output verification must not reuse the tolerant source path.
					parent, err := out.DereferenceDict(root["ParentTree"])
					if err != nil {
						t.Fatal(err)
					}
					nums, err := out.DereferenceArray(parent["Nums"])
					if err != nil {
						t.Fatal(err)
					}
					parent["Nums"] = append(nums, types.Integer(10000), ids["PC1_identity0"])
					if parent["Limits"] != nil {
						parent["Limits"] = types.Array{types.Integer(0), types.Integer(10000)}
					}
					root["ParentTreeNextKey"] = types.Integer(10001)
					var damaged bytes.Buffer
					if err = serialize(t.Context(), out, &damaged); err != nil {
						t.Fatal(err)
					}
					if _, err = readOutput(t.Context(), damaged.Bytes()); err == nil {
						t.Fatal("reopened output silently normalized a stale index")
					}
				}
			})
		}
	}
}

func TestUnsafeParentIndexesAreNotNormalized(t *testing.T) {
	for _, kind := range []string{"claimed-key", "duplicate-owner", "live-array-slot", "wrong-owner", "duplicate-key", "cycle", "unreachable-target"} {
		t.Run(kind, func(t *testing.T) {
			d := staleParentIndexFixture(false, false)
			switch kind {
			case "claimed-key":
				d.Objs[3] = bytes.Replace(d.Objs[3], []byte("/StructParents 0"), []byte("/StructParents 0 /Annots [19 0 R]"), 1)
				d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [10 10 40 40] /StructParent 3 /A << /S /URI /URI (https://example.invalid/) >> >>"))
			case "duplicate-owner":
				d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/StructParents 1"), []byte("/StructParents 0"), 1)
			case "live-array-slot":
				d.Objs[10] = bytes.Replace(d.Objs[10], []byte("0 [12 0 R]"), []byte("0 [12 0 R 12 0 R]"), 1)
			case "wrong-owner":
				d.Objs[10] = bytes.Replace(d.Objs[10], []byte("0 [12 0 R]"), []byte("0 [13 0 R]"), 1)
			case "duplicate-key":
				d.Objs[10] = bytes.Replace(d.Objs[10], []byte("3 12 0 R"), []byte("3 12 0 R 3 13 0 R"), 1)
			case "cycle":
				d.Objs[10] = []byte("<< /Kids [11 0 R] >>")
			case "unreachable-target":
				d.Objs = append(d.Objs, []byte("<< /Type /StructElem /S /P /P 10 0 R /Pg 4 0 R /K 0 >>"))
				d.Objs[10] = bytes.Replace(d.Objs[10], []byte("3 12 0 R"), []byte("3 19 0 R"), 1)
			}
			_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "invalid" {
				t.Fatalf("unsafe source index accepted: %+v", ds)
			}
			_, slots, ds := Inspect(t.Context(), d.Bytes(), packet.StructureDiscard, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "ok" || slots[0] == nil || slots[0].StructureCoverage != "none" {
				t.Fatalf("explicit discard still depended on source index validity: %+v", ds)
			}
		})
	}
}

func TestAbsentIDIndexRebuiltWithoutLosingHeaderBindings(t *testing.T) {
	d := pdffixture.MultipageTable()
	d.Objs[9] = bytes.Replace(d.Objs[9], []byte("/IDTree 15 0 R"), nil, 1)
	expectIndexNormalization(t, d, "IDTree")
	for _, fit := range []*packet.Size{nil, {Width: 300, Height: 400}} {
		path, ds := selectedPacket(t, d, []packet.Item{&packet.SourceItem{Ref: "first", Path: "source"}, &packet.SourceItem{Ref: "second", Path: "source"}}, fit)
		if status := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out, err := readOutput(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		root, err := structureRoot(out)
		if err != nil {
			t.Fatal(err)
		}
		ids, err := structureIDTree(t.Context(), out, root["IDTree"])
		if err != nil || len(ids) != 6 {
			t.Fatalf("repeated IDs lost: %v %v", ids, err)
		}
		for _, prefix := range []string{"PC1_", "PC2_"} {
			cell, err := out.DereferenceDict(ids[prefix+"cell2"])
			if err != nil {
				t.Fatal(err)
			}
			attributes, err := out.DereferenceDict(cell["A"])
			if err != nil {
				t.Fatal(err)
			}
			headers, err := out.DereferenceArray(attributes["Headers"])
			if err != nil || len(headers) != 1 {
				t.Fatalf("header association lost: %v %v", headers, err)
			}
			id, err := structureID(out, headers[0])
			if err != nil || id != prefix+"header" || ids[id] == nil {
				t.Fatalf("header association crossed occurrences: %q %v", id, err)
			}
		}
	}
	_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "selected", Pages: []packet.PageRange{{First: 3, Last: 3}}}})
	if status := diag.Outcome(ds); status != "invalid" {
		t.Fatalf("index rebuilding hid omitted table header: %+v", ds)
	}
}

func TestAbsentIDIndexDoesNotRepairContradictoryOrAmbiguousIDs(t *testing.T) {
	for _, kind := range []string{"wrong-target", "empty-present", "duplicate-id", "missing-header"} {
		t.Run(kind, func(t *testing.T) {
			d := pdffixture.MultipageTable()
			switch kind {
			case "wrong-target":
				d.Objs[14] = bytes.Replace(d.Objs[14], []byte("(header) 12 0 R"), []byte("(header) 13 0 R"), 1)
			case "empty-present":
				d.Objs[14] = []byte("<< >>")
			case "duplicate-id":
				d.Objs[9] = bytes.Replace(d.Objs[9], []byte("/IDTree 15 0 R"), nil, 1)
				d.Objs[13] = bytes.Replace(d.Objs[13], []byte("/ID (cell2)"), []byte("/ID (header)"), 1)
			case "missing-header":
				d.Objs[9] = bytes.Replace(d.Objs[9], []byte("/IDTree 15 0 R"), nil, 1)
				d.Objs[13] = bytes.Replace(d.Objs[13], []byte("/Headers [(header)]"), []byte("/Headers [(missing)]"), 1)
			}
			_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "invalid" {
				t.Fatalf("invalid ID relationships repaired: %+v", ds)
			}
			_, slots, ds := Inspect(t.Context(), d.Bytes(), packet.StructureDiscard, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "ok" || slots[0] == nil || slots[0].StructureCoverage != "none" {
				t.Fatalf("discard depended on ID relationships: %+v", ds)
			}
		})
	}
}

// An indirect reference to a null placeholder has the same meaning as a direct
// null. Neither claims a nonexistent MCID, including in strict reopened output.
func TestParentIndexNullPlaceholdersResolveAtEveryBoundary(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("indirect=%v", indirect), func(t *testing.T) {
			d := taggedSelectionFixture()
			null := "null"
			if indirect {
				d.Objs = append(d.Objs, []byte("null"))
				null = "19 0 R"
			}
			d.Objs[10] = bytes.Replace(d.Objs[10], []byte("0 [12 0 R]"), []byte("0 [12 0 R "+null+"]"), 1)
			_, slots, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "ok" || slots[0] == nil {
				t.Fatalf("legal null placeholder refused: %+v", ds)
			}
			if _, err := readOutput(t.Context(), d.Bytes()); err != nil {
				t.Fatalf("strict output treats null reference as ownership: %v", err)
			}
		})
	}
}
