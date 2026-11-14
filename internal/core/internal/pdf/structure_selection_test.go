// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func taggedSelectionFixture() *pdffixture.Doc {
	d := pdffixture.Resource(3, func(i int) string { return fmt.Sprintf("page%d", i+1) })
	insert := func(index int, entry string) {
		raw := d.Objs[index-1]
		end := bytes.LastIndex(raw, []byte(">>"))
		d.Objs[index-1] = append(append(append([]byte{}, raw[:end]...), []byte(" "+entry+" ")...), raw[end:]...)
	}
	stream := func(extra string, data []byte) []byte {
		return []byte(fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", extra, len(data), data))
	}
	insert(1, "/MarkInfo << /Marked true >> /Lang (en) /StructTreeRoot 10 0 R")
	d.Objs = append(d.Objs, []byte("<< /Type /StructTreeRoot /K [12 0 R 13 0 R 14 0 R] /ParentTree 11 0 R /IDTree 18 0 R /RoleMap << /Local /P >> /ClassMap << /Region << /O /Layout /BBox [10 20 100 120] >> >> >>"), []byte("<< /Nums [0 [12 0 R] 1 [13 0 R] 2 [14 0 R]] >>"))
	for i := range 3 {
		page := 4 + 2*i
		d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /StructElem /S /Local /P 10 0 R /Pg %d 0 R /K 0 /C /Region /Lang (lv) /ID (identity%d) /Alt (alt%d) /ActualText (semantic%d) >>", page, i, i, i)))
	}
	for i := range 3 {
		page, content := 4+2*i, 5+2*i
		raw := d.Objs[content-1]
		start := bytes.Index(raw, []byte("stream\n")) + 7
		end := bytes.LastIndex(raw, []byte("\nendstream"))
		body := bytes.Clone(raw[start:end])
		// Both page streams share one page MCID namespace and one marked sequence.
		d.Objs[content-1] = stream("", append([]byte("/P <</MCID 0>> BDC\n"), body...))
		d.Objs = append(d.Objs, stream("", []byte("EMC\n")))
		d.Objs[page-1] = bytes.ReplaceAll(d.Objs[page-1], []byte(fmt.Sprintf("/Contents %d 0 R", content)), []byte(fmt.Sprintf("/Contents [%d 0 R %d 0 R]", content, 15+i)))
		// Use a simple known frame so the independent transformed expectation is half.
		raw = d.Objs[page-1]
		a := bytes.Index(raw, []byte("/MediaBox ["))
		b := a + bytes.IndexByte(raw[a:], ']') + 1
		d.Objs[page-1] = append(append(append([]byte{}, raw[:a]...), []byte("/MediaBox [0 0 600 800]")...), raw[b:]...)
		insert(page, fmt.Sprintf("/StructParents %d", i))
	}
	d.Objs = append(d.Objs, []byte("<< /Names [(identity0) 12 0 R (identity1) 13 0 R (identity2) 14 0 R] >>"))
	return d
}

func TestTaggedSelectionRepetitionAndFittedClasses(t *testing.T) {
	tools := oracle.RequireTools(t)
	d := taggedSelectionFixture()
	path, ds := selectedPacket(t, d, []packet.Item{&packet.SourceItem{Ref: "selected", Path: "source", Selection: []packet.PageRange{{First: 1, Last: 1}, {First: 3, Last: 3}}}, &packet.SourceItem{Ref: "repeated", Path: "source", Selection: []packet.PageRange{{First: 3, Last: 3}}}}, &packet.Size{Width: 300, Height: 400})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "semantic1") || strings.Contains(string(raw), "identity1") {
		t.Fatal("omitted semantic content survived")
	}
	for _, want := range []string{"semantic0", "semantic2", "PC1_identity0", "PC2_identity2", "PC1_Local", "PC2_Local", "FitPage"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing retained semantic fact %q", want)
		}
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	objects := map[string]any{}
	qpdf, _ := document["qpdf"].([]any)
	for _, section := range qpdf {
		entries, _ := section.(map[string]any)
		for key, entry := range entries {
			if strings.HasPrefix(key, "obj:") {
				wrapper, _ := entry.(map[string]any)
				objects[strings.TrimPrefix(key, "obj:")] = wrapper["value"]
			}
		}
	}
	dict := func(value any) map[string]any {
		if ref, ok := value.(string); ok {
			value = objects[ref]
		}
		d, _ := value.(map[string]any)
		return d
	}
	var classMap map[string]any
	for _, object := range objects {
		d := dict(object)
		if d["/Type"] == "/StructTreeRoot" {
			classMap = dict(d["/ClassMap"])
		}
	}
	count := 0
	for _, object := range objects {
		d := dict(object)
		if name, ok := d["/C"].(string); ok {
			attributes := dict(classMap[name])
			box, _ := attributes["/BBox"].([]any)
			if fmt.Sprint(box) != "[5 10 50 60]" {
				t.Fatalf("fitted class geometry: %v", box)
			}
			count++
		}
	}
	if count != 3 {
		t.Fatalf("wanted three retained class consumers, got %d", count)
	}
	if err = tools.Check(path); err != nil {
		t.Fatal(err)
	}
	// A malformed parent tree on omitted page 2 is still refused before selection.
	bad := taggedSelectionFixture()
	bad.Objs[10] = bytes.Replace(bad.Objs[10], []byte("1 [13 0 R]"), []byte("1 [12 0 R]"), 1)
	_, _, findings := Inspect(t.Context(), bad.Bytes(), []SelectionRequest{{Item: "selected", Pages: []packet.PageRange{{First: 1, Last: 1}}}})
	status, _ := diag.Outcome(findings)
	if status != "invalid" {
		t.Fatalf("omitted-page structure corruption escaped: %+v", findings)
	}
}

func TestPDF20NamespaceRolesKeepTheirScope(t *testing.T) {
	tools := oracle.RequireTools(t)
	source := pdffixture.Tagged("namespace")
	source.Version = "2.0"
	source.Objs[5] = bytes.Replace(source.Objs[5], []byte("/Type /StructTreeRoot"), []byte("/Type /StructTreeRoot /RoleMap << /Local /P >> /Namespaces [9 0 R]"), 1)
	source.Objs[6] = bytes.Replace(source.Objs[6], []byte("/S /P"), []byte("/S /Local /NS 9 0 R"), 1)
	source.Objs = append(source.Objs, []byte("<< /Type /Namespace /NS (urn:pdfconcat:namespace-fixture) /RoleMapNS << /Local /Local >> >>"))
	source.Objs = append(source.Objs, []byte("/Local"), []byte("/P"), []byte("/Namespace"))
	source.Objs[8] = bytes.Replace(source.Objs[8], []byte("/Type /Namespace"), []byte("/Type 12 0 R"), 1)
	source.Objs[6] = bytes.Replace(source.Objs[6], []byte("/S /Local"), []byte("/S 10 0 R"), 1)
	source.Objs[5] = bytes.Replace(source.Objs[5], []byte("/Local /P"), []byte("/Local 11 0 R"), 1)
	source.Objs[8] = bytes.Replace(source.Objs[8], []byte("/Local /Local"), []byte("/Local 10 0 R"), 1)
	path, ds := selectedPacket(t, source, []packet.Item{&packet.SourceItem{Ref: "first", Path: "source"}, &packet.SourceItem{Ref: "repeat", Path: "source"}}, &packet.Size{Width: 300, Height: 400})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"/S": "/Local"`) != 2 || !strings.Contains(string(raw), `"/Local": "/PC1_Local"`) || !strings.Contains(string(raw), `"/Local": "/PC2_Local"`) {
		t.Fatalf("namespace roles lost source scope: %s", raw)
	}
	source.Objs[8] = bytes.Replace(source.Objs[8], []byte("/Local 10 0 R"), []byte("/Local [/Local 9 0 R]"), 1)
	_, _, findings := Inspect(t.Context(), source.Bytes(), nil)
	if status, _ := diag.Outcome(findings); status != "invalid" {
		t.Fatalf("namespace cycle escaped: %+v", findings)
	}
}

func TestTaggedSourceDoesNotClaimCoverageForUnownedAnnotation(t *testing.T) {
	source := pdffixture.Tagged("paragraph")
	source.Objs[2] = bytes.Replace(source.Objs[2], []byte("/StructParents 0"), []byte("/StructParents 0 /Annots [9 0 R]"), 1)
	source.Objs = append(source.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [10 10 20 20] /P 3 0 R /Dest [3 0 R /Fit] >>"))
	facts, _, ds := Inspect(t.Context(), source.Bytes(), nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	if facts.StructureCoverage != "partial" {
		t.Fatalf("unowned annotation was reported as %s coverage", facts.StructureCoverage)
	}
}

func TestObjectReferenceMustBelongToItsDeclaredPage(t *testing.T) {
	source := taggedSelectionFixture()
	source.Objs[5] = bytes.Replace(source.Objs[5], []byte("/StructParents 1"), []byte("/StructParents 1 /Annots [19 0 R]"), 1)
	source.Objs[11] = bytes.Replace(source.Objs[11], []byte("/K 0"), []byte("/K [0 << /Type /OBJR /Pg 4 0 R /Obj 19 0 R >>]"), 1)
	source.Objs[10] = bytes.Replace(source.Objs[10], []byte("2 [14 0 R]"), []byte("2 [14 0 R] 3 12 0 R"), 1)
	source.Objs = append(source.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [10 10 20 20] /P 6 0 R /Dest [4 0 R /Fit] /StructParent 3 >>"))
	_, _, ds := Inspect(t.Context(), source.Bytes(), nil)
	if len(ds) != 1 || ds[0].Code != diag.SourceInvalid || !strings.Contains(ds[0].Detail, "declared page") {
		t.Fatalf("annotation page mismatch not detected: %+v", ds)
	}
}

func TestSerializedCoverageIsCheckedAgainstResolvedExpectation(t *testing.T) {
	source := pdffixture.Tagged("paragraph")
	source.Objs[2] = bytes.Replace(source.Objs[2], []byte("/StructParents 0"), []byte("/StructParents 0 /Annots [9 0 R]"), 1)
	source.Objs = append(source.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [10 10 20 20] /P 3 0 R /Dest [3 0 R /Fit] >>"))
	path, ds := selectedPacket(t, source, []packet.Item{&packet.SourceItem{Ref: "source", Path: "source"}}, nil)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	expected := Assembly{Structure: packet.StructurePreserve, Coverage: "complete"}
	err := VerifyAssembly(t.Context(), path, 1, expected)
	if err == nil || Diagnostic(err).Code != diag.OutputInvalid || !strings.Contains(Diagnostic(err).Detail, "coverage") {
		t.Fatalf("false complete coverage escaped serialized verification: %v", err)
	}
	expected.Coverage = "partial"
	if err = VerifyAssembly(t.Context(), path, 1, expected); err != nil {
		t.Fatal(err)
	}
}

func TestIndirectStructureBindingsSurviveAssembly(t *testing.T) {
	for _, fit := range []*packet.Size{nil, {Width: 300, Height: 400}} {
		for _, array := range []bool{false, true} {
			source := taggedSelectionFixture()
			// Alias -> indirect Local -> P; indirect C -> names/revision array with an
			// indirect member. These are all valid PDF object representations.
			source.Objs = append(source.Objs, []byte("/Region"), []byte("/Local"), []byte("/P"))
			source.Objs[9] = bytes.Replace(source.Objs[9], []byte("/Local /P"), []byte("/Alias 20 0 R /Local 21 0 R"), 1)
			for _, index := range []int{11, 12, 13} {
				source.Objs[index] = bytes.Replace(source.Objs[index], []byte("/S /Local"), []byte("/S 20 0 R"), 1)
			}
			if array {
				source.Objs = append(source.Objs, []byte("[19 0 R 0]"))
				for _, index := range []int{11, 12, 13} {
					source.Objs[index] = bytes.Replace(source.Objs[index], []byte("/C /Region"), []byte("/C 22 0 R"), 1)
				}
			} else {
				for _, index := range []int{11, 12, 13} {
					source.Objs[index] = bytes.Replace(source.Objs[index], []byte("/C /Region"), []byte("/C 19 0 R"), 1)
				}
			}
			path, ds := selectedPacket(t, source, []packet.Item{&packet.SourceItem{Ref: "first", Path: "source"}, &packet.SourceItem{Ref: "again", Path: "source"}}, fit)
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out, err := readOutput(t.Context(), data)
			if err != nil {
				t.Fatal(err)
			}
			root, err := structureRoot(out)
			if err != nil {
				t.Fatal(err)
			}
			roles, err := out.DereferenceDict(root["RoleMap"])
			if err != nil {
				t.Fatal(err)
			}
			classes, err := out.DereferenceDict(root["ClassMap"])
			if err != nil {
				t.Fatal(err)
			}
			if roles["PC1_Alias"] != types.Name("PC1_Local") || roles["PC1_Local"] != types.Name("P") {
				t.Fatalf("indirect role targets lost: %v", roles)
			}
			consumers := 0
			if err = structureElements(t.Context(), out, root["K"], nil, func(d types.Dict, _ types.Object) error {
				if d["C"] == nil {
					return nil
				}
				consumers++
				role, e := nameValue(out, d["S"])
				if e != nil {
					return e
				}
				if role != "PC1_Local" && role != "PC2_Local" {
					return fmt.Errorf("role not resolved: %s", role)
				}
				_, e = mapAttributeClasses(out, d["C"], func(name types.Name) (types.Object, error) {
					attrs, e := out.DereferenceDict(classes[string(name)])
					if e != nil {
						return nil, e
					}
					if attrs == nil {
						return nil, fmt.Errorf("unbound class %s", name)
					}
					box, e := out.DereferenceArray(attrs["BBox"])
					if e != nil {
						return nil, e
					}
					want := [4]float64{10, 20, 100, 120}
					if fit != nil {
						want = [4]float64{5, 10, 50, 60}
					}
					if len(box) != 4 {
						return nil, fmt.Errorf("class bounds length: %v", box)
					}
					for i, value := range box {
						number, e := out.DereferenceNumber(value)
						if e != nil {
							return nil, e
						}
						if number != want[i] {
							return nil, fmt.Errorf("class attribute changed: %v", box)
						}
					}
					return name, nil
				}, 0)
				return e
			}, 0); err != nil {
				t.Fatal(err)
			}
			if consumers != 6 {
				t.Fatalf("retained class consumers=%d", consumers)
			}
			// A missing binding must be refused on reopened output, rather than accepted
			// merely because the PDF parses and its parent tree is internally consistent.
			delete(classes, "PC1_Region")
			if fit == nil {
				if err = validateStructure(t.Context(), out); err == nil {
					t.Fatal("unbound output class accepted")
				}
			}
			out, err = readOutput(t.Context(), data)
			if err != nil {
				t.Fatal(err)
			}
			root, err = structureRoot(out)
			if err != nil {
				t.Fatal(err)
			}
			roles, err = out.DereferenceDict(root["RoleMap"])
			if err != nil {
				t.Fatal(err)
			}
			delete(roles, "PC1_Local")
			if err = validateStructure(t.Context(), out); err == nil {
				t.Fatal("unbound output role accepted")
			}

		}
	}
}
