// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func assembleSourceBytes(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	out := filepath.Join(dir, "output.pdf")
	facts, _, ds := pdf.Inspect(t.Context(), data, "", nil)
	if status := diag.Outcome(ds); status != "ok" {
		t.Fatalf("inspect: %+v", ds)
	}
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	layout, _ := resolvePacket(pdffixture.Plan(t, packet.Declaration{Output: "", Fitting: nil, PageNumbers: nil, PageDefaults: packet.PageStyle{}, Items: []packet.Item{&packet.SourceItem{Path: source, Ref: source}}}), map[string]packet.SourceFacts{source: facts.SourceFacts})
	var b bytes.Buffer
	if _, err := pdf.Assemble(t.Context(), testAssembly(t, layout, map[string]pdf.SourceFacts{source: facts}, wholeOccurrences(layout, map[string]pdf.SourceFacts{source: facts}), nil, nil, nil), &b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := pdf.Verify(t.Context(), readPDF(t, out), layout.TotalPages); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestIndirectDecodeParametersAndGenerationIdentity(t *testing.T) {
	tools := oracle.RequireTools(t)
	d := pdffixture.Plain("indirect-decode")
	raw := d.Objs[3]
	start := bytes.Index(raw, []byte("stream\n")) + len("stream\n")
	end := bytes.LastIndex(raw, []byte("\nendstream"))
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(raw[start:end]); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	d.Objs[3] = []byte(fmt.Sprintf("<< /Length %d /Filter /FlateDecode /DecodeParms 6 0 R >>\nstream\n%s\nendstream", compressed.Len(), compressed.Bytes()))
	d.Objs = append(d.Objs, []byte("<< /Predictor 1 >>"))
	path := assembleSourceBytes(t, d.Bytes())
	document, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	if document.FirstLine(1) != "indirect-decode" {
		t.Fatal("indirect DecodeParms lost content")
	}

	d = pdffixture.Plain("generation-one")
	data := d.Bytes()
	data = bytes.Replace(data, []byte("4 0 obj"), []byte("4 1 obj"), 1)
	data = bytes.Replace(data, []byte("/Contents 4 0 R"), []byte("/Contents 4 1 R"), 1)
	lines := bytes.Split(data, []byte{'\n'})
	for i, line := range lines {
		if string(line) == "xref" {
			lines[i+2+4] = bytes.Replace(lines[i+2+4], []byte("00000 n"), []byte("00001 n"), 1)
			break
		}
	}
	path = assembleSourceBytes(t, bytes.Join(lines, []byte{'\n'}))
	document, err = oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	if document.FirstLine(1) != "generation-one" {
		t.Fatal("nonzero generation lost content")
	}
}

func TestFreeObjectRecyclingPreservesDestinations(t *testing.T) {
	tools := oracle.RequireTools(t)
	data := pdffixture.NamedDests("recycled").Bytes()
	lines := bytes.Split(data, []byte{'\n'})
	for i, line := range lines {
		if string(line) == "xref" {
			// Object 5 is the unused placeholder in this fixture. Reuse it for rebuilt destinations.
			lines[i+2] = []byte("0000000005 65535 f ")
			lines[i+2+5] = []byte("0000000000 00001 f ")
			break
		}
	}
	path := assembleSourceBytes(t, bytes.Join(lines, []byte{'\n'}))
	if err := tools.Check(path); err != nil {
		t.Fatal(err)
	}
	doc, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range map[int]int{1: 2, 2: 1} {
		targets, err := doc.ActionTargets(page)
		if err != nil || len(targets) != 1 || targets[0] != want {
			t.Fatalf("recycled output target: %v %v", targets, err)
		}
	}
}

func TestModelNumericSerializationRoundTripsExactly(t *testing.T) {
	tools := oracle.RequireTools(t)
	d := pdffixture.WithBoxes("precision", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 612.123456789012345 792]", UserUnit: "1.123456789012345"})
	path := assembleSourceBytes(t, d.Bytes())
	raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("qpdf returned no facts")
	}
	pages := qpdfPages(t, path)
	native := nativePageFacts(pages[0])
	if native["/UserUnit"] != float64(1.123456789012345) {
		t.Fatalf("UserUnit lost precision: %+v", native)
	}
	box, ok := native["/MediaBox"].([]any)
	if !ok || len(box) != 4 {
		t.Fatal("missing numeric MediaBox", native)
	}
	if box[2] != float64(612.123456789012345) {
		t.Fatalf("MediaBox lost precision: %+v", box)
	}

	document, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	if document.FirstLine(1) != "precision p1" {
		t.Fatal("numeric boundary lost visible content")
	}
}

func TestLiteralHashFontResourceRetainsVisibleText(t *testing.T) {
	tools := oracle.RequireTools(t)
	d := pdffixture.Plain("literal-hash-font")
	for i, body := range d.Objs {
		d.Objs[i] = bytes.ReplaceAll(body, []byte("/F1"), []byte("/F#23A"))
	}
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{d}, nil)
	if status := diag.Outcome(ds); status != "ok" {
		t.Fatalf("ordinary literal hash: %+v", ds)
	}
	doc, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.FirstLine(1) != "literal-hash-font" {
		t.Fatal("literal-hash resource lost visible text")
	}
}

func TestWrongGenerationCannotExposeContent(t *testing.T) {
	d := pdffixture.Plain("WRONG-GENERATION-CONTENT")
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Contents 4 0 R"), []byte("/Contents 4 1 R"))
	_, _, ds := pdf.Inspect(t.Context(), d.Bytes(), "", nil)
	if diag.Outcome(ds) != "invalid" || !codePresent(ds, diag.SourceDamaged) {
		t.Fatalf("wrong-generation content requires attributable refusal: %+v", ds)
	}
	if len(ds) == 0 || ds[0].PDFLocation == nil {
		t.Fatalf("missing original site: %+v", ds)
	}
}
