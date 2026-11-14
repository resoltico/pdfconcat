// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func tableHeaderFixture(class, indirect bool) *pdffixture.Doc {
	d := pdffixture.MultipageTable()
	if indirect {
		d.Objs = append(d.Objs, []byte("<< /O 21 0 R /Headers 22 0 R >>"), []byte("/Table"), []byte("[23 0 R]"), []byte("<686561646572>"), []byte("0"), []byte("[20 0 R 24 0 R]"))
		d.Objs[13] = bytes.Replace(d.Objs[13], []byte("/A << /O /Table /Headers [(header)] >>"), []byte("/A 25 0 R"), 1)
		d.Objs[11] = bytes.Replace(d.Objs[11], []byte("/ID (header)"), []byte("/ID 23 0 R"), 1)
	}
	if class {
		attr := "<< /O /Table /Headers [(header)] >>"
		if indirect {
			attr = "25 0 R"
			d.Objs[24] = []byte("[20 0 R]") // ClassMap arrays contain dictionaries, not revisions.
		}
		d.Objs[9] = bytes.Replace(d.Objs[9], []byte("/IDTree"), []byte("/ClassMap << /HeaderAssociation "+attr+" >> /IDTree"), 1)
		consumer := "/C /HeaderAssociation"
		if indirect {
			d.Objs = append(d.Objs, []byte("/HeaderAssociation"))
			consumer = "/C [26 0 R 24 0 R]"
		}
		d.Objs[13] = bytes.Replace(d.Objs[13], []byte("/A "+attr), []byte(consumer), 1)
	}
	// Resolve a custom header role through an indirect default RoleMap target.
	d.Objs = append(d.Objs, []byte("/TH"))
	d.Objs[9] = bytes.Replace(d.Objs[9], []byte("/IDTree"), []byte(fmt.Sprintf("/RoleMap << /Header %d 0 R >> /IDTree", len(d.Objs))), 1)
	d.Objs[11] = bytes.Replace(d.Objs[11], []byte("/S /TH"), []byte("/S /Header"), 1)
	return d
}

func TestTableHeaderSelectionsAndSerializedBindings(t *testing.T) {
	for _, class := range []bool{false, true} {
		for _, indirect := range []bool{false, true} {
			for _, fit := range []bool{false, true} {
				t.Run(fmt.Sprintf("class=%v/indirect=%v/fit=%v", class, indirect, fit), func(t *testing.T) {
					d := tableHeaderFixture(class, indirect)
					var size *packet.Size
					if fit {
						size = &packet.Size{Width: 300, Height: 400}
					}
					_, slots, ds := Inspect(t.Context(), d.Bytes(), []SelectionRequest{{Item: "invalid", Pages: []packet.PageRange{{First: 3, Last: 3}}, FitTo: size}})
					status, _ := diag.Outcome(ds)
					if status != "invalid" || slots[0] != nil {
						t.Fatalf("dangling selection accepted: %+v", ds)
					}
					if len(ds) != 1 || ds[0].Code != diag.SourceUnsupported || !strings.Contains(ds[0].Message, "Include the referenced header pages") {
						t.Fatalf("missing remedy: %+v", ds)
					}
					for _, selection := range [][]packet.PageRange{nil, {{First: 1, Last: 1}, {First: 3, Last: 3}}} {
						parts := []packet.Item{&packet.SourceItem{Ref: "a", Path: "source", Selection: selection}, &packet.SourceItem{Ref: "b", Path: "source", Selection: selection}}
						path, ds := selectedPacket(t, d, parts, size)
						if len(ds) > 0 {
							t.Fatal(ds)
						}
						raw, e := os.ReadFile(path)
						if e != nil {
							t.Fatal(e)
						}
						out, e := readOutput(t.Context(), raw)
						if e != nil {
							t.Fatal(e)
						}
						root, e := structureRoot(out)
						if e != nil {
							t.Fatal(e)
						}
						ids, e := structureIDTree(t.Context(), out, root["IDTree"])
						if e != nil {
							t.Fatal(e)
						}
						for _, prefix := range []string{"PC1_", "PC2_"} {
							if ids[prefix+"header"] == nil || ids[prefix+"cell2"] == nil {
								t.Fatalf("missing occurrence IDs: %v", ids)
							}
						}
						// Check exact associations independently of the binding validator. Include
						// class/revision arrays by walking raw resolved attribute representations.
						consumers := 0
						classes, e := out.DereferenceDict(root["ClassMap"])
						if e != nil {
							t.Fatal(e)
						}
						var attributes func(types.Object) error
						expectedHeader := ""
						attributes = func(o types.Object) error {
							v, e := out.Dereference(o)
							if e != nil {
								return e
							}
							switch a := v.(type) {
							case types.Array:
								for _, x := range a {
									if e = attributes(x); e != nil {
										return e
									}
								}
							case types.Dict:
								if a["Headers"] != nil {
									headers, e := out.DereferenceArray(a["Headers"])
									if e != nil {
										return e
									}
									if len(headers) != 1 {
										return fmt.Errorf("headers: %v", headers)
									}
									id, e := structureID(out, headers[0])
									if e != nil {
										return e
									}
									if id != expectedHeader {
										return fmt.Errorf("unrenamed header %q", id)
									}
									if ids[id] == nil {
										return fmt.Errorf("dangling header %q", id)
									}
									consumers++
								}
							}
							return nil
						}
						e = structureElements(t.Context(), out, root["K"], nil, func(d types.Dict, _ types.Object) error {
							expectedHeader = ""
							if d["ID"] != nil {
								id, e := structureID(out, d["ID"])
								if e != nil {
									return e
								}
								if strings.HasSuffix(id, "cell2") {
									expectedHeader = strings.TrimSuffix(id, "cell2") + "header"
								}
							}
							if e := attributes(d["A"]); e != nil {
								return e
							}
							if d["C"] != nil {
								_, e := mapAttributeClasses(out, d["C"], func(n types.Name) (types.Object, error) { return n, attributes(classes[string(n)]) }, 0)
								return e
							}
							return nil
						}, 0)
						if e != nil {
							t.Fatal(e)
						}
						if consumers != 2 {
							t.Fatalf("consumers=%d", consumers)
						}
						// Corrupt only a semantic association, leaving IDs and their tree valid.
						cell, e := out.DereferenceDict(ids["PC1_cell2"])
						if e != nil {
							t.Fatal(e)
						}
						broken := types.Dict{"O": types.Name("Table"), "Headers": types.Array{types.StringLiteral("missing")}}
						if class {
							_, e = mapAttributeClasses(out, cell["C"], func(n types.Name) (types.Object, error) { classes[string(n)] = broken; return n, nil }, 0)
							if e != nil {
								t.Fatal(e)
							}
						} else {
							cell["A"] = broken
						}
						var damaged bytes.Buffer
						if e = serialize(t.Context(), out, &damaged); e != nil {
							t.Fatal(e)
						}
						if _, e = readOutput(t.Context(), damaged.Bytes()); e == nil || !strings.Contains(e.Error(), "table Headers") {
							t.Fatalf("serialized dangling relation accepted: %v", e)
						}
					}
				})
			}
		}
	}
}

func TestMalformedOriginalTableHeadersCannotHideBehindSelection(t *testing.T) {
	for _, class := range []bool{false, true} {
		for _, kind := range []string{"missing", "wrong-role", "name", "id-name", "cycle"} {
			t.Run(fmt.Sprintf("class=%v/%s", class, kind), func(t *testing.T) {
				d := tableHeaderFixture(class, true)
				switch kind {
				case "missing":
					d.Objs[22] = []byte("(missing)")
				case "wrong-role":
					d.Objs[11] = bytes.Replace(d.Objs[11], []byte("/S /Header"), []byte("/S /TD"), 1)
				case "name":
					d.Objs[22] = []byte("/header")
				case "id-name":
					d.Objs[11] = bytes.Replace(d.Objs[11], []byte("/ID 23 0 R"), []byte("/ID /header"), 1)
				case "cycle":
					d.Objs[24] = []byte("[25 0 R]")
				}
				_, _, ds := Inspect(t.Context(), d.Bytes(), []SelectionRequest{{Item: "unrelated", Pages: []packet.PageRange{{First: 2, Last: 2}}}})
				status, _ := diag.Outcome(ds)
				if status != "invalid" {
					t.Fatalf("malformed original hidden: %+v", ds)
				}
			})
		}
	}
}

func TestNamespacedTableHeaderBindings(t *testing.T) {
	for _, mapping := range []bool{false, true} {
		t.Run(fmt.Sprintf("mapping=%v", mapping), func(t *testing.T) {
			d := pdffixture.MultipageTable()
			d.Version = "2.0"
			name, ns := "TH", 20
			d.Objs = append(d.Objs, []byte("<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>"), []byte("<< /Type /Namespace /NS (urn:table-test) /RoleMapNS << /Heading [/TH 20 0 R] >> >>"))
			if mapping {
				name = "Heading"
				ns = 21
			}
			d.Objs[9] = bytes.Replace(d.Objs[9], []byte("/IDTree"), []byte("/Namespaces [20 0 R 21 0 R] /IDTree"), 1)
			d.Objs[11] = bytes.Replace(d.Objs[11], []byte("/S /TH"), []byte(fmt.Sprintf("/S /%s /NS %d 0 R", name, ns)), 1)
			_, ds := selectedPacket(t, d, []packet.Item{&packet.SourceItem{Ref: "a", Path: "source"}, &packet.SourceItem{Ref: "b", Path: "source"}}, &packet.Size{Width: 300, Height: 400})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
		})
	}
}
