// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestIntegratedDocumentFeatures(t *testing.T) {
	tools := oracle.RequireTools(t)
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := pdffixture.Tagged(name).WriteFile(filepath.Join(dir, name+".pdf")); err != nil {
			t.Fatal(err)
		}
	}
	font, err := filepath.Abs("testdata/fonts/source-serif/SourceSerif4-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	plan := map[string]any{"version": 5, "metadata": map[string]any{"title": "Rīga <&> report", "author": "One; authored string", "subject": "Έκθεση", "language": "lv"}, "timestamp": 0, "fit_to": "A4", "page_defaults": map[string]any{"size": "A4", "text": map[string]any{"font": map[string]any{"file": font}}}, "page_numbers": map[string]any{"mode": "consecutive", "start": 9, "position": "top-right", "kinds": []string{"source", "divider"}}, "items": []any{
		map[string]any{"page": map[string]any{"text": map[string]any{"value": "Rīgas office report"}}, "numbering": false}, map[string]any{"contents": map[string]any{}}, map[string]any{"title": "Annex", "divider": map[string]any{}, "items": []any{map[string]any{"source": "a.pdf"}, map[string]any{"source": "b.pdf", "numbering": false}, map[string]any{"source": "a.pdf"}}},
	}}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var hashes []string
	for _, name := range []string{"one.pdf", "two.pdf"} {
		r := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanBytes{Data: data}, WorkingDir: dir, Output: name})
		if r.Status != "ok" || !r.Output.Published {
			t.Fatalf("build %s: %+v", name, r.Diagnostics)
		}
		if r.Structure == nil || r.Structure.Coverage == nil || *r.Structure.Coverage != "complete" {
			t.Fatalf("coverage: %+v", r.Structure)
		}
		hashes = append(hashes, r.Output.SHA256)
	}
	if hashes[0] != hashes[1] {
		t.Fatal("output destination changed PDF bytes")
	}
	path := filepath.Join(dir, "one.pdf")
	if err = tools.Check(path); err != nil {
		t.Fatal(err)
	}
	text, err := exec.CommandContext(t.Context(), tools.PDFToText, "-raw", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Rīgas office report", "Annex", "9", "10", "11", "a", "b"} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/StructTreeRoot"`, `"/ParentTree"`, `"/CIDFontType0"`, `"/DisplayDocTitle": true`, `"/PageLabels"`, `"/Stm"`, `"/OBJR"`, `u:en`, `u:lv`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("serialized fact %s missing", want)
		}
	}
	// Explicit discard removes claims and ownership, while preserving visible text.
	plan["structure"] = "discard"
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	r := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanBytes{Data: data}, WorkingDir: dir, Output: "discard.pdf"})
	if r.Status != "ok" {
		t.Fatal(r.Diagnostics)
	}
	raw, err = exec.CommandContext(t.Context(), tools.QPDF, "--json", filepath.Join(dir, "discard.pdf")).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"/StructTreeRoot"`, `"/MarkInfo"`, `"/StructParents"`, `"/StructParent"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("discard retained %s", key)
		}
	}
}

func TestMetadataTimestampsAcrossPDFVersions(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, version := range []string{"1.7", "2.0"} {
		for _, epoch := range []any{nil, int64(0), int64(253402300799)} {
			dir := t.TempDir()
			if err := pdffixture.Versioned("source", version, "").WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
				t.Fatal(err)
			}
			plan := map[string]any{"version": 5, "metadata": map[string]any{"title": "<Rīga & report>", "author": "One, authored; string", "language": "x-private"}, "items": []any{map[string]any{"source": "source.pdf"}}}
			if epoch != nil {
				plan["timestamp"] = epoch
			}
			data, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			r := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanBytes{Data: data}, WorkingDir: dir, Output: "out.pdf"})
			if r.Status != "ok" {
				t.Fatalf("%s timestamp %v: %+v", version, epoch, r.Diagnostics)
			}
			if err = tools.Check(filepath.Join(dir, "out.pdf")); err != nil {
				t.Fatal(err)
			}
			bytes, err := os.ReadFile(filepath.Join(dir, "out.pdf"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(bytes), "pdfuaid") {
				t.Fatal("unexpected PDF/UA claim")
			}
		}
	}
}
