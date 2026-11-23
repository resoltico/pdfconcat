// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func admissionStream(extra, program string) []byte {
	return []byte(fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", extra, len(program), program))
}

func admissionEntry(doc *pdffixture.Doc, object int, entry string) *pdffixture.Doc {
	raw := doc.Objs[object-1]
	last := bytes.LastIndex(raw, []byte(">>"))
	if last < 0 {
		panic("authored fixture object is not a dictionary")
	}
	doc.Objs[object-1] = append(append(append([]byte{}, raw[:last]...), []byte(" "+entry+" ")...), raw[last:]...)
	return doc
}

// This authored producer-shaped document deliberately has no hierarchy. A page,
// a shared Form and a link annotation carry abandoned ownership keys; page 1's
// and the Form's MCID 0 values must not become one semantic namespace.
func orphanAdmissionFixture(second string) *pdffixture.Doc {
	page := "/P << /MCID 0 >> BDC BT /F1 18 Tf 30 340 Td (PAGE) Tj ET EMC\nq /Shared Do Q\nq 1 0 0 1 0 -40 cm /Shared Do Q\n"
	form := "/Span << /MCID 0 >> BDC BT /F1 18 Tf 30 280 Td (FORM) Tj ET EMC\n"
	return &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Resources << /Font << /F1 5 0 R >> /XObject << /Shared 8 0 R >> >> /Contents 4 0 R /Annots [9 0 R] /StructParents 0 >>"),
		admissionStream("", page),
		[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Resources << >> /Contents 7 0 R /StructParents 99 >>"),
		admissionStream("", second),
		admissionStream("/Type /XObject /Subtype /Form /BBox [0 0 300 400] /Resources << /Font << /F1 5 0 R >> >> /StructParents 23 /StructParent 24", form),
		[]byte("<< /Type /Annot /Subtype /Link /Rect [30 330 90 355] /Border [0 0 0] /StructParent 40 /A << /S /URI /URI (https://example.invalid/reference) >> >>"),
	}}
}

func admissionFinding(t *testing.T, ds []diag.Diagnostic, code diag.Code) {
	t.Helper()
	for _, d := range ds {
		if d.Code == code {
			if d.PDFLocation == nil || d.PDFLocation.Object < 1 || d.PDFLocation.Path == "" {
				t.Fatalf("unlocated %s: %+v", code, d)
			}
			return
		}
	}
	t.Fatalf("missing %s: %+v", code, ds)
}

func admissionError(t *testing.T, ds []diag.Diagnostic) diag.Diagnostic {
	t.Helper()
	for _, d := range ds {
		if d.Severity == diag.Error {
			return d
		}
	}
	t.Fatalf("missing error: %+v", ds)
	return diag.Diagnostic{}
}

func TestStructureAdmissionDiscardPrecedesSemanticValidation(t *testing.T) {
	for _, kind := range []string{"parent", "cycle", "type", "mark-info", "owner"} {
		t.Run(kind, func(t *testing.T) {
			d := pdffixture.Tagged("retained")
			switch kind {
			case "parent":
				d.Objs[6] = bytes.Replace(d.Objs[6], []byte("/P 6 0 R"), []byte("/P 99 0 R"), 1)
			case "cycle":
				d.Objs[6] = bytes.Replace(d.Objs[6], []byte("/K 0"), []byte("/K 7 0 R"), 1)
			case "type":
				d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/Type /StructTreeRoot"), []byte("/Type 42"), 1)
			case "mark-info":
				d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/MarkInfo << /Marked true >>"), []byte("/MarkInfo 42"), 1)
			case "owner":
				d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/StructParents 0"), []byte("/StructParents (abandoned)"), 1)
			}
			broad := &packet.FitPolicy{Target: packet.Size{Width: 600, Height: 800}, Tolerance: packet.MaxSide}
			_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "retained", Fitting: broad}})
			if status := diag.Outcome(ds); status != "invalid" {
				t.Fatalf("preserve accepted malformed hierarchy: %+v", ds)
			}
			finding := admissionError(t, ds)
			if finding.PDFLocation == nil || finding.PDFLocation.Object < 1 || finding.PDFLocation.Path == "" || !strings.Contains(strings.ToLower(finding.Message), "discard") {
				t.Fatalf("missing structure location/remedy: %+v", finding)
			}
			before := parsedFixture(t, d)
			witness, err := capturePageContent(t.Context(), before)
			if err != nil {
				t.Fatal(err)
			}
			after, _, findings, err := readSource(t.Context(), d.Bytes(), packet.StructureDiscard)
			if err != nil {
				t.Fatal(err)
			}
			admissionFinding(t, findings, diag.SourceStructureRemoved)
			if err = witness.verify(t.Context(), after); err != nil {
				t.Fatal("discard changed encoded page content", err)
			}
			if after.Tagged {
				t.Fatal("discard left the backend tagged cache set")
			}
			if err = walkObjects(t.Context(), after, func(d types.Dict) error {
				if d["StructParent"] != nil || d["StructParents"] != nil {
					return errors.New("ownership survived discard")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			cat, err := after.Catalog()
			if err != nil {
				t.Fatal(err)
			}
			if cat["StructTreeRoot"] != nil || cat["MarkInfo"] != nil {
				t.Fatal("discard retained catalog claims")
			}
		})
	}
}

// assembleAdmission exercises actual-byte re-admission, repeated selected
// occurrences and a new output root supplied by generated or tagged content.
func assembleAdmission(t *testing.T, d *pdffixture.Doc, policy packet.StructurePolicy, selected int, fit *packet.FitPolicy, companion string) (string, *model.Context) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	if err := d.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	requests := []SelectionRequest{{Item: "first", Pages: []packet.PageRange{{First: selected, Last: selected}}, Fitting: fit}, {Item: "repeat", Pages: []packet.PageRange{{First: selected, Last: selected}}, Fitting: fit}}
	file, slots, ds := Inspect(t.Context(), d.Bytes(), policy, requests)
	if status := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	code := diag.SourceStructureOrphaned
	if policy == packet.StructureDiscard {
		code = diag.SourceStructureRemoved
	}
	admissionFinding(t, ds, code)
	views := map[string]OccurrenceFacts{}
	domain := map[string]packet.SourceFacts{}
	files := map[string]SourceFacts{source: file}
	var items []packet.Item
	for i, request := range requests {
		if slots[i] == nil {
			t.Fatal("missing selected occurrence")
		}
		views[request.Item] = *slots[i]
		domain[request.Item] = slots[i].SourceFacts
	}
	var generated *typeset.Placed
	if companion == "generated" {
		font, err := typeset.LoadDefaultFont()
		if err != nil {
			t.Fatal(err)
		}
		var shaper typeset.Shaper
		generated, err = shaper.Place(font, typeset.Params{Text: "GENERATED", Size: 18, PageWidth: 300, PageHeight: 400, WrapWidth: 240, LineSpacing: 1.2, Anchor: typeset.AnchorTopLeft, Overflow: typeset.OverflowReject})
		if err != nil {
			t.Fatal(err)
		}
		style := packet.Defaults()
		style.Size.Value = packet.Size{Width: 300, Height: 400}
		style.Size.Present = true
		items = append(items, &packet.PageItem{Ref: "generated", Style: style, Count: 1})
	}
	if companion == "tagged" {
		doc := pdffixture.Tagged("TAGGED")
		path := filepath.Join(dir, "tagged.pdf")
		if err := doc.WriteFile(path); err != nil {
			t.Fatal(err)
		}
		f, v, findings := Inspect(t.Context(), doc.Bytes(), policy, []SelectionRequest{{Item: "tagged", Fitting: fit}})
		if status := diag.Outcome(findings); status != "ok" {
			t.Fatal(findings)
		}
		files[path] = f
		views["tagged"] = *v[0]
		domain["tagged"] = v[0].SourceFacts
		items = append(items, &packet.SourceItem{Ref: "tagged", Path: path})
	}
	for _, request := range requests {
		items = append(items, &packet.SourceItem{Ref: request.Item, Path: source, Selection: request.Pages})
	}
	plan := pdffixture.Plan(t, packet.Declaration{Output: "", Fitting: fit, PageNumbers: nil, PageDefaults: packet.PageStyle{}, Items: items})
	draft := plan.Declaration()
	draft.Structure = policy
	plan = pdffixture.Plan(t, draft)
	layout, ds := resolvePacket(plan, domain)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	assembly := testAssembly(t, layout, files, views, fit)
	assembly.Structure = policy
	if generated != nil {
		fonts, err := typeset.PrepareFonts([]*typeset.Placed{generated})
		if err != nil {
			t.Fatal(err)
		}
		assembly.Fonts = fonts
		assembly.Parts[0].Generated = []compose.Page{{Width: 300, Height: 400, Blocks: []compose.TextBlock{{Role: compose.RoleParagraph, Text: generated}}}}
	}
	var buffer bytes.Buffer
	if _, err := Assemble(t.Context(), assembly, &buffer); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := readOutput(t.Context(), buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = Verify(t.Context(), readPDF(t, path), layout.TotalPages); err != nil {
		t.Fatal(err)
	}
	return path, reopened
}

func TestStructureAdmissionOrphansAcrossPoliciesSelectionAndFitting(t *testing.T) {
	for _, policy := range []packet.StructurePolicy{packet.StructurePreserve, packet.StructureDiscard} {
		for _, fitted := range []bool{false, true} {
			for _, companion := range []string{"none", "generated", "tagged"} {
				for _, content := range []string{"meaningful", "blank", "artifact"} {
					t.Run(fmt.Sprintf("%s/fit=%v/%s/%s", policy, fitted, companion, content), func(t *testing.T) {
						second := ""
						if content == "artifact" {
							second = "/Artifact BMC 0.5 g 20 20 30 30 re f EMC\n"
						}
						d := orphanAdmissionFixture(second)
						page := 1
						if content != "meaningful" {
							page = 2
						}
						var fit *packet.FitPolicy
						if fitted {
							fit = packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400})
						}
						_, slots, ds := Inspect(t.Context(), d.Bytes(), policy, []SelectionRequest{{Item: "selected", Pages: []packet.PageRange{{First: page, Last: page}}, Fitting: fit}})
						if status := diag.Outcome(ds); status != "ok" {
							t.Fatal(ds)
						}
						if slots[0].HasUncoveredContent != (content == "meaningful") || slots[0].StructureCoverage != "none" {
							t.Fatalf("source coverage: %+v", slots[0])
						}
						_, out := assembleAdmission(t, d, policy, page, fit, companion)
						marked, err := markedContent(t.Context(), out)
						if err != nil {
							t.Fatal(err)
						}
						expected := "none"
						if policy == packet.StructurePreserve && companion != "none" {
							expected = "complete"
							if content == "meaningful" {
								expected = "partial"
							}
						}
						if marked.coverage() != expected {
							t.Fatalf("reopened coverage=%s; want %s", marked.coverage(), expected)
						}
						if policy == packet.StructureDiscard {
							cat, err := out.Catalog()
							if err != nil {
								t.Fatal(err)
							}
							if cat["StructTreeRoot"] != nil || cat["MarkInfo"] != nil {
								t.Fatal("discard output has structure claims")
							}
						}
					})
				}
			}
		}
	}
}

func TestStructureAdmissionPreservesVisibleContentAndNavigation(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, policy := range []packet.StructurePolicy{packet.StructurePreserve, packet.StructureDiscard} {
		for _, fitted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fit=%v", policy, fitted), func(t *testing.T) {
				d := orphanAdmissionFixture("")
				var fit *packet.FitPolicy
				if fitted {
					fit = packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400})
				}
				output, _ := assembleAdmission(t, d, policy, 1, fit, "generated")
				source := filepath.Join(t.TempDir(), "original.pdf")
				if err := d.WriteFile(source); err != nil {
					t.Fatal(err)
				}
				actual, err := oracle.Load(tools, output)
				if err != nil {
					t.Fatal(err)
				}
				original, err := oracle.Load(tools, source)
				if err != nil {
					t.Fatal(err)
				}
				texts, err := actual.RawTexts()
				if err != nil {
					t.Fatal(err)
				}
				for _, page := range []int{2, 3} {
					if got := strings.Fields(texts[page-1]); !slices.Equal(got, []string{"PAGE", "FORM", "FORM"}) {
						t.Fatalf("source text changed: %q", got)
					}
					difference, e := actual.AppearanceDifference(page, original, 1, 0)
					if e != nil || difference != 0 {
						t.Fatalf("source pixels changed: %v %v", difference, e)
					}
				}
				raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", output).Output()
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(string(raw), "https://example.invalid/reference") != 2 {
					t.Fatal("repeated link annotations/navigation changed")
				}
				if err = tools.Check(output); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestStructureAdmissionDiscardKeepsGeneralProtections(t *testing.T) {
	cases := []struct {
		name string
		doc  func() *pdffixture.Doc
		code diag.Code
	}{
		{"missing page", func() *pdffixture.Doc {
			d := pdffixture.Tagged("page")
			d.Objs[1] = []byte("<< /Type /Pages /Count 1 /Kids [999 0 R] >>")
			return d
		}, diag.SourceDamaged},
		{"missing content", func() *pdffixture.Doc {
			d := pdffixture.Tagged("content")
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Contents 4 0 R"), []byte("/Contents 999 0 R"), 1)
			return d
		}, diag.SourceDamaged},
		{"encoded content", func() *pdffixture.Doc {
			program := compressedContent(t, []byte("BT /F1 12 Tf 10 20 Td (ENCODED) Tj ET"))
			return pdffixture.EncodedPages("broken", []pdffixture.EncodedStream{{Data: program[:len(program)-1], Filter: "/FlateDecode"}})
		}, diag.SourceInvalid},
		{"signature", func() *pdffixture.Doc { return pdffixture.SignatureValue("signed") }, diag.SourceSigned},
		{"signature hidden in discarded tree", func() *pdffixture.Doc {
			d := pdffixture.Tagged("hidden")
			admissionEntry(d, 6, "/Private 9 0 R")
			d.Objs = append(d.Objs, []byte("<< /Type /Sig /ByteRange [0 1 2 3] /Contents <00> >>"))
			return d
		}, diag.SourceSigned},
		{"unreachable signature", func() *pdffixture.Doc {
			d := pdffixture.Tagged("unreachable")
			d.Objs = append(d.Objs, []byte("<< /Type /Sig /ByteRange [0 1 2 3] /Contents <00> >>"))
			return d
		}, diag.SourceSigned},
		{"layers hidden in discarded tree", func() *pdffixture.Doc {
			d := pdffixture.Tagged("layers")
			admissionEntry(d, 6, "/Private << /Type /OCG /Name (Shadow group) >>")
			return admissionEntry(d, 1, "/OCProperties << /OCGs [] /D << /Order [] >> >>")
		}, diag.SourceOptionalContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.doc()
			broad := &packet.FitPolicy{Target: packet.Size{Width: 600, Height: 800}, Tolerance: packet.MaxSide}
			_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructureDiscard, []SelectionRequest{{Item: "bad", Fitting: broad}})
			finding := admissionError(t, ds)
			if finding.Code != tc.code {
				t.Fatalf("discard bypassed/general failure changed: %+v", finding)
			}
			path := filepath.Join(t.TempDir(), "bad.pdf")
			if err := d.WriteFile(path); err != nil {
				t.Fatal(err)
			}
			// Even a forged admission cache cannot waive checks of the actual bytes.
			facts, _, cleanDS := Inspect(t.Context(), pdffixture.Plain("admitted cache").Bytes(), packet.StructureDiscard, nil)
			if diag.Outcome(cleanDS) != "ok" {
				t.Fatal(cleanDS)
			}
			// Forge only the content identity; retain an internally consistent prepared
			// geometry owner so this control reaches actual-byte re-admission.
			facts.SHA256 = sha256.Sum256(d.Bytes())
			view := facts.OccurrenceFacts
			plan := pdffixture.Plan(t, packet.Declaration{Output: "", Fitting: nil, PageNumbers: nil, PageDefaults: packet.PageStyle{}, Items: []packet.Item{&packet.SourceItem{Ref: "bad", Path: path}}})
			layout, findings := resolvePacket(plan, map[string]packet.SourceFacts{"bad": view.SourceFacts})
			if len(findings) > 0 {
				t.Fatal(findings)
			}
			assembly := testAssembly(t, layout, map[string]SourceFacts{path: facts}, map[string]OccurrenceFacts{"bad": view}, nil)
			assembly.Structure = packet.StructureDiscard
			var output bytes.Buffer
			_, err := Assemble(t.Context(), assembly, &output)
			if err == nil || Diagnostic(err).Code != tc.code || output.Len() != 0 {
				t.Fatalf("assembly bypassed actual-byte protection: %v, %d bytes", err, output.Len())
			}
		})
	}
}

// A Form's unowned MCID cannot satisfy a page's forward semantic reference.
func TestStructureAdmissionFormMCIDsDoNotBelongToInvokingPage(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrong-forward=%v", wrong), func(t *testing.T) {
			d := orphanAdmissionFixture("")
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/Pages 2 0 R"), []byte("/Pages 2 0 R /StructTreeRoot 10 0 R"), 1)
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/Annots [9 0 R]"), nil, 1)
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/StructParents 99"), nil, 1)
			d.Objs[7] = bytes.Replace(d.Objs[7], []byte(" /StructParents 23 /StructParent 24"), nil, 1)
			d.Objs[8] = bytes.Replace(d.Objs[8], []byte(" /StructParent 40"), nil, 1)
			key := "0"
			parents := "[12 0 R]"
			if wrong {
				key = "1"
				parents = "[null 12 0 R]"
				d.Objs[3] = admissionStream("", "/P << /MCID 0 >> BDC BT /F1 18 Tf 30 340 Td (PAGE) Tj ET EMC q /Shared Do Q")
				d.Objs[7] = admissionStream("/Type /XObject /Subtype /Form /BBox [0 0 300 400] /Resources << /Font << /F1 5 0 R >> >>", "/Span << /MCID 1 >> BDC BT /F1 18 Tf 30 280 Td (FORM) Tj ET EMC")
			}
			d.Objs = append(d.Objs, []byte("<< /Type /StructTreeRoot /K 12 0 R /ParentTree 11 0 R >>"), []byte("<< /Nums [0 "+parents+"] >>"), []byte("<< /Type /StructElem /S /P /P 10 0 R /Pg 3 0 R /K "+key+" >>"))
			_, slots, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if wrong {
				if status := diag.Outcome(ds); status != "invalid" {
					t.Fatalf("Form MCID satisfied wrong page reference: %+v", ds)
				}
			} else {
				if status := diag.Outcome(ds); status != "ok" {
					t.Fatal(ds)
				}
				if slots[0].StructureCoverage != "partial" || !slots[0].HasUncoveredContent {
					t.Fatalf("unowned Form coverage: %+v", slots[0])
				}
			}
		})
	}
}

func TestStructureAdmissionDiscardKeepsFormBoundaries(t *testing.T) {
	d := pdffixture.Form("FORM")
	admissionEntry(d, 1, "/MarkInfo << /Marked true >>")
	admissionEntry(d, 3, "/StructParents 0")
	_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructureDiscard, []SelectionRequest{{Item: "selected", Pages: []packet.PageRange{{First: 1, Last: 1}}}})
	if got := admissionError(t, ds); got.Code != diag.SourceSelectionFormUnsupported {
		t.Fatalf("discard bypassed partial form protection: %+v", got)
	}
	f, views, ds := Inspect(t.Context(), d.Bytes(), packet.StructureDiscard, []SelectionRequest{{Item: "whole", Fitting: packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400})}})
	if admissionError(t, ds).Code != diag.FitFormUnsupported {
		t.Fatal(ds)
	}
	if !f.HasWidgets || !f.HasFields {
		t.Fatal("tag cleanup removed form identity")
	}
	a := Assembly{SourcePolicies: SourcePolicies{Structure: packet.StructureDiscard}, Fitting: packet.DefaultFitPolicy(packet.Size{Width: 300, Height: 400}), Parts: []Part{{Part: packet.Part{Leaf: packet.Leaf{Ref: "whole", Kind: packet.PartSource, Source: "source.pdf"}}, Occurrence: &Occurrence{File: &f, Facts: views[0]}}}}
	findings := CheckCompatibility(a).Diagnostics
	if got := admissionError(t, findings); got.Code != diag.FitFormUnsupported {
		t.Fatalf("discard bypassed form fitting protection: %+v", got)
	}
}

func TestStructureAdmissionDiscardKeepsEncryptionBoundary(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, password := range []string{"", "secret"} {
		t.Run(fmt.Sprintf("empty-password=%v", password == ""), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "encrypted.pdf")
			if err := pdffixture.Tagged("ENCRYPTED").WriteFile(path); err != nil {
				t.Fatal(err)
			}
			if err := tools.Encrypt(path, password); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			broad := &packet.FitPolicy{Target: packet.Size{Width: 600, Height: 800}, Tolerance: packet.MaxSide}
			_, _, ds := Inspect(t.Context(), data, packet.StructureDiscard, []SelectionRequest{{Item: "encrypted", Fitting: broad}})
			if got := admissionError(t, ds); got.Code != diag.SourceEncrypted {
				t.Fatalf("discard bypassed encryption: %+v", got)
			}
		})
	}
}

func TestStructureAdmissionTreeLessClaimKinds(t *testing.T) {
	for _, kind := range []string{"mark-info", "page", "form", "annotation", "mcid-only"} {
		t.Run(kind, func(t *testing.T) {
			d := orphanAdmissionFixture("")
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte(" /MarkInfo << /Marked true >>"), nil, 1)
			d.Objs[2] = bytes.Replace(d.Objs[2], []byte(" /StructParents 0"), nil, 1)
			d.Objs[5] = bytes.Replace(d.Objs[5], []byte(" /StructParents 99"), nil, 1)
			d.Objs[7] = bytes.Replace(d.Objs[7], []byte(" /StructParents 23 /StructParent 24"), nil, 1)
			d.Objs[8] = bytes.Replace(d.Objs[8], []byte(" /StructParent 40"), nil, 1)
			switch kind {
			case "mark-info":
				admissionEntry(d, 1, "/MarkInfo << /Marked true >>")
			case "page":
				admissionEntry(d, 3, "/StructParents (orphan)")
			case "form":
				d.Objs[7] = bytes.Replace(d.Objs[7], []byte("/Length"), []byte("/StructParents (orphan) /Length"), 1)
			case "annotation":
				admissionEntry(d, 9, "/StructParent (orphan)")
			case "mcid-only": // No ownership dictionaries or MarkInfo exist at all.
			}
			_, views, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "ok" {
				t.Fatal(ds)
			}
			admissionFinding(t, ds, diag.SourceStructureOrphaned)
			if views[0].StructureCoverage != "none" || !views[0].HasUncoveredContent {
				t.Fatalf("orphan claims became semantics: %+v", views[0])
			}
		})
	}
}

func TestStructureAdmissionDiscardPreservesSharedCatalogAndInfo(t *testing.T) {
	d := pdffixture.Tagged("SHARED")
	d.Info = "<< /Title (KEEP INFO) >>"
	d.Objs[5] = bytes.Replace(d.Objs[5], []byte("/Type /StructTreeRoot"), []byte("/Type 42"), 1)
	// These references are semantic-only edges, not semantic-only objects. The
	// catalog and Info retain independent trailer identities after detachment.
	admissionEntry(d, 6, "/CatalogEcho 1 0 R /InfoEcho 9 0 R /PageEcho 3 0 R /FontEcho 5 0 R")
	before := parsedFixture(t, d)
	witness, err := capturePageContent(t.Context(), before)
	if err != nil {
		t.Fatal(err)
	}
	after, _, ds, err := readSource(t.Context(), d.Bytes(), packet.StructureDiscard)
	if err != nil {
		t.Fatal(err)
	}
	admissionFinding(t, ds, diag.SourceStructureRemoved)
	cat, err := after.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if cat == nil || cat["Type"] != types.Name("Catalog") || after.Root.ObjectNumber.Value() != 1 || after.Table[1].Free {
		t.Fatal("semantic cleanup freed the retained catalog")
	}
	if after.Info == nil || after.Info.ObjectNumber.Value() != 9 || after.Table[9].Free {
		t.Fatal("semantic cleanup freed the retained Info object")
	}
	info, err := after.DereferenceDict(*after.Info)
	if err != nil {
		t.Fatal(err)
	}
	title, err := after.DereferenceText(info["Title"])
	if err != nil || title != "KEEP INFO" {
		t.Fatalf("retained Info changed: %q %v", title, err)
	}
	if err = witness.verify(t.Context(), after); err != nil {
		t.Fatal("shared retained graph changed content", err)
	}
}

func TestStructureAdmissionNullOwnershipIsAbsent(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("indirect=%v", indirect), func(t *testing.T) {
			d := pdffixture.URILink("NULL OWNERS", "https://example.invalid/null", "", false)
			value := "null"
			if indirect {
				d.Objs = append(d.Objs, []byte("null"))
				value = "7 0 R"
			}
			admissionEntry(d, 3, "/StructParents "+value)
			admissionEntry(d, 6, "/StructParent "+value)
			_, views, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "ok" {
				t.Fatal(ds)
			}
			for _, finding := range ds {
				if finding.Code == diag.SourceStructureOrphaned {
					t.Fatalf("null optional ownership became a structure claim: %+v", finding)
				}
			}
			if views[0].StructureCoverage != "none" || !views[0].HasUncoveredContent {
				t.Fatalf("null ownership supplied semantics: %+v", views[0])
			}
		})
	}
}

func TestStructureAdmissionSignatureNamesCannotMaskEachOther(t *testing.T) {
	for _, tc := range []struct {
		fields           string
		literalCollision bool
	}{
		{"/Type /Sig /Ty#2370e /P /ByteRange [0 1 2 3] /Contents <00>", true},
		{"/Type /P /Ty#2370e /Sig /ByteRange [0 1 2 3] /Contents <00>", true},
		{"/FT /Sig /F#2354 /Tx /V <00>", true},
		{"/FT /Sig /V <00> /#2356 null", false},
	} {
		fields := tc.fields
		t.Run(fields, func(t *testing.T) {
			d := pdffixture.Tagged("SIGNATURE")
			admissionEntry(d, 6, "/Private 9 0 R")
			d.Objs = append(d.Objs, []byte("<< "+fields+" >>"))
			// Non-null literal-hash keys survive parsing and collide on a second decode.
			// The null-valued V control may be omitted by the parser, but must still
			// refuse the populated signature. Never let map iteration choose a winner.
			parsed := parsedFixture(t, d)
			object, err := parsed.DereferenceDict(*types.NewIndirectRef(9, 0))
			if err != nil {
				t.Fatal(err)
			}
			hasLiteralHash := false
			for name := range object {
				if strings.Contains(name, "#") {
					hasLiteralHash = true
				}
			}
			if tc.literalCollision && !hasLiteralHash {
				t.Fatal("fixture no longer exercises decoded-name collision")
			}
			for range 16 {
				_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructureDiscard, []SelectionRequest{{Item: "signature"}})
				finding := admissionError(t, ds)
				if finding.Code != diag.SourceSigned || finding.PDFLocation == nil || finding.PDFLocation.Object != 9 {
					t.Fatalf("signature candidate was masked: %+v", ds)
				}
			}
		})
	}
}

func TestStructureAdmissionOptionalStructureMetadata(t *testing.T) {
	for _, marked := range []string{"false", "null"} {
		t.Run("indirect-marked-"+marked, func(t *testing.T) {
			d := pdffixture.Plain("OPTIONAL")
			d.Objs = append(d.Objs, []byte(marked))
			admissionEntry(d, 1, "/MarkInfo << /Marked 6 0 R >>")
			_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "ok" {
				t.Fatal(ds)
			}
			for _, finding := range ds {
				if finding.Code == diag.SourceStructureOrphaned {
					t.Fatalf("absent/false Marked became semantic claim: %+v", finding)
				}
			}
		})
	}
	for _, indirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("null-next-key-indirect=%v", indirect), func(t *testing.T) {
			d := pdffixture.Tagged("OPTIONAL")
			value := "null"
			if indirect {
				d.Objs = append(d.Objs, []byte("null"))
				value = "9 0 R"
			}
			admissionEntry(d, 6, "/ParentTreeNextKey "+value)
			_, views, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
			if status := diag.Outcome(ds); status != "ok" {
				t.Fatal(ds)
			}
			if views[0].StructureCoverage != "complete" {
				t.Fatalf("optional null key changed coverage: %+v", views[0])
			}
		})
	}
	// Null is not a substitute for a real namespace when a forward content edge
	// actually requires that owner. This remains genuine hierarchy damage.
	d := pdffixture.Tagged("REQUIRED")
	d.Objs = append(d.Objs, []byte("null"))
	d.Objs[2] = bytes.Replace(d.Objs[2], []byte("/StructParents 0"), []byte("/StructParents 9 0 R"), 1)
	_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "required"}})
	if status := diag.Outcome(ds); status != "invalid" {
		t.Fatalf("null waived a required forward owner: %+v", ds)
	}
}

func TestStructureAdmissionMissingAnnotationBindingIsLocated(t *testing.T) {
	d := pdffixture.Tagged("ANNOTATION")
	admissionEntry(d, 3, "/Annots [9 0 R]")
	d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [30 330 90 355] /Border [0 0 0] /StructParent 4 /A << /S /URI /URI (https://example.invalid/binding) >> >>"))
	_, _, ds := Inspect(t.Context(), d.Bytes(), packet.StructurePreserve, []SelectionRequest{{Item: "source"}})
	finding := admissionError(t, ds)
	if finding.Code != diag.SourceInvalid || finding.PDFLocation == nil || finding.PDFLocation.Object != 9 || !strings.Contains(finding.PDFLocation.Path, "StructParent") {
		t.Fatalf("missing annotation binding lost its object/key attribution: %+v", finding)
	}
}
