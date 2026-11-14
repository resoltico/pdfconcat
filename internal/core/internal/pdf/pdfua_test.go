// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

// This specifically authored fixture declares conformance only in test code.
// Production never certifies accessibility or copies source conformance claims.
func TestPDFUAMachineFixture(t *testing.T) {
	tool, err := exec.LookPath("verapdf")
	if err != nil {
		if os.Getenv("PDFCONCAT_REQUIRE_VERAPDF") == "1" {
			t.Fatal("veraPDF is required")
		}
		t.Skip("veraPDF unavailable; human reading-order validation remains separate")
	}
	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}
	var shaper typeset.Shaper
	placed, err := shaper.Place(font, typeset.Params{Text: "An accessible paragraph.", Size: 12, PageWidth: 300, PageHeight: 400, LineSpacing: 1.2})
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := generatedContext(t.Context(), []compose.Page{{Width: 300, Height: 400, Blocks: []compose.TextBlock{{Role: compose.RoleParagraph, Text: placed}}}}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	original, err := structureRoot(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = combineStructure(t.Context(), s, []structurePart{{root: original}}); err != nil {
		t.Fatal(err)
	}
	title, lang := "Authored PDF/UA machine fixture", "en"
	if err = setDocumentMetadata(s, &packet.Metadata{Title: &title, Language: &lang}, nil); err != nil {
		t.Fatal(err)
	}
	cat, err := s.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	object, err := s.Dereference(cat["Metadata"])
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := object.(types.StreamDict)
	if !ok {
		t.Fatal("missing XMP")
	}
	data, err := decodeDrawingStream(t.Context(), s, metadata)
	if err != nil {
		t.Fatal(err)
	}
	claim := `<rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"><pdfuaid:part>1</pdfuaid:part></rdf:Description>`
	data = bytes.Replace(data, []byte("</rdf:RDF>"), []byte(claim+"</rdf:RDF>"), 1)
	ref, err := addStream(s, data, types.Dict{"Type": types.Name("Metadata"), "Subtype": types.Name("XML")})
	if err != nil {
		t.Fatal(err)
	}
	cat["Metadata"] = *ref
	var out bytes.Buffer
	if err = serialize(t.Context(), s, &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.pdf")
	if err = os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	validate := func(path, label string) bool {
		t.Helper()
		raw, err := exec.CommandContext(t.Context(), tool, "--flavour", "ua1", "--format", "xml", path).CombinedOutput()
		var exited *exec.ExitError
		if err != nil && (!errors.As(err, &exited) || exited.ExitCode() != 1) {
			t.Fatalf("veraPDF process: %v\n%s", err, raw)
		}
		if e := os.WriteFile(filepath.Join(t.ArtifactDir(), label+".xml"), raw, 0600); e != nil {
			t.Fatal(e)
		}
		var report struct {
			Jobs struct {
				Job struct {
					Validation struct {
						Compliant *bool  `xml:"isCompliant,attr"`
						Status    string `xml:"jobEndStatus,attr"`
					} `xml:"validationReport"`
				} `xml:"job"`
			} `xml:"jobs"`
		}
		if err = xml.Unmarshal(raw, &report); err != nil {
			t.Fatalf("veraPDF report: %v\n%s", err, raw)
		}
		if report.Jobs.Job.Validation.Compliant == nil || report.Jobs.Job.Validation.Status != "normal" {
			t.Fatalf("veraPDF did not validate fixture: %s", raw)
		}
		if !*report.Jobs.Job.Validation.Compliant && label != "missing-structure" {
			t.Logf("veraPDF noncompliance: %s", raw)
		}
		return *report.Jobs.Job.Validation.Compliant
	}
	if !validate(path, "valid") {
		t.Fatal("authored fixture failed PDF/UA machine checks")
	}
	// Preserve two occurrences of this independently machine-checked source,
	// including fitting. Output must omit the source conformance declaration.
	facts, _, findings := Inspect(t.Context(), out.Bytes(), nil)
	if status, _ := diag.Outcome(findings); status != "ok" {
		t.Fatal(findings)
	}
	occurrence := &Occurrence{File: &facts, Facts: &facts.OccurrenceFacts}
	assembly := Assembly{Metadata: &packet.Metadata{Title: &title, Language: &lang}, Structure: packet.StructurePreserve, FitTo: &packet.Size{Width: 450, Height: 600}, Parts: []Part{
		{Part: packet.Part{Leaf: packet.Leaf{Kind: packet.PartSource, Ref: "first", Source: path}, Pages: 1, FirstPage: 1, LastPage: 1}, Occurrence: occurrence},
		{Part: packet.Part{Leaf: packet.Leaf{Kind: packet.PartSource, Ref: "repeat", Source: path}, Pages: 1, FirstPage: 2, LastPage: 2}, Occurrence: occurrence},
	}}
	var merged bytes.Buffer
	if _, err = Assemble(t.Context(), assembly, &merged); err != nil {
		t.Fatal(err)
	}
	s, err = readOutput(t.Context(), merged.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	cat, err = s.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	object, err = s.Dereference(cat["Metadata"])
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok = object.(types.StreamDict)
	if !ok {
		t.Fatal("missing output XMP")
	}
	data, err = decodeDrawingStream(t.Context(), s, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("pdfuaid")) {
		t.Fatal("source PDF/UA claim was inherited")
	}
	data = bytes.Replace(data, []byte("</rdf:RDF>"), []byte(claim+"</rdf:RDF>"), 1)
	ref, err = addStream(s, data, types.Dict{"Type": types.Name("Metadata"), "Subtype": types.Name("XML")})
	if err != nil {
		t.Fatal(err)
	}
	cat["Metadata"] = *ref
	out.Reset()
	if err = serialize(t.Context(), s, &out); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "fitted-repeated.pdf")
	if err = os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if !validate(path, "fitted-repeated") {
		t.Fatal("fitted repeated source failed PDF/UA machine checks")
	}
	// Negative control: remove semantic ownership while preserving the appearance.
	delete(cat, "StructTreeRoot")
	delete(cat, "MarkInfo")
	out.Reset()
	if err = serialize(t.Context(), s, &out); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if validate(path, "missing-structure") {
		t.Fatal("veraPDF accepted removed structure")
	}
}
