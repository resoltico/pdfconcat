// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func assembleSourceBytes(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	out := filepath.Join(dir, "output.pdf")
	facts, ds := pdf.Inspect(t.Context(), data)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatalf("inspect: %+v", ds)
	}
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	layout, _ := job.Resolve(job.Job{Parts: []job.Part{{Source: source}}}, map[string]job.SourceFacts{source: facts.SourceFacts})
	var b bytes.Buffer
	if _, err := pdf.Assemble(t.Context(), layout, map[string]pdf.SourceFacts{source: facts}, nil, nil, nil, &b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := pdf.Verify(t.Context(), out, layout.TotalPages); err != nil {
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

func TestUpstreamNumericSerializationLimitIsExplicit(t *testing.T) {
	tools := oracle.RequireTools(t)
	d := pdffixture.WithBoxes("precision", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 612.123456789012345 792]", UserUnit: "1.123456789012345"})
	path := assembleSourceBytes(t, d.Bytes())
	raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "1.123456789012") {
		t.Fatalf("expected observed upstream 12-fraction-digit serialization: %s", raw)
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
	if status, _ := diag.Outcome(ds); status != "ok" {
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
