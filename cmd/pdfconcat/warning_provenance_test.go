// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	exchange "github.com/resoltico/pdfconcat/internal/wire"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestLocatedWarningsSurviveIndependentSummaryCapsAndLaterFailure(t *testing.T) {
	dir := t.TempDir()
	d := pdffixture.Resource(2, func(i int) string { return fmt.Sprintf("PAGE %d", i+1) })
	var refs strings.Builder
	for range 25 {
		object := len(d.Objs) + 1
		fmt.Fprintf(&refs, "%d 0 R ", object)
		d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Link /Rect [10 10 30 30] /Dest [4 0 R /Fit] >>"))
	}
	page := d.Objs[5]
	end := bytes.LastIndex(page, []byte(">>"))
	d.Objs[5] = append(append(append([]byte{}, page[:end]...), []byte(" /Annots ["+refs.String()+"] ")...), page[end:]...)
	source := filepath.Join(dir, "source.pdf")
	if err := d.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	items := []any{}
	for range 22 {
		items = append(items, map[string]any{"source": "source.pdf", "pages": [][2]int{{2, 2}}})
	}
	plan := map[string]any{"version": 7, "items": items}
	for _, laterFailure := range []bool{false, true} {
		if laterFailure {
			if err := os.WriteFile(filepath.Join(dir, "bad.pdf"), []byte("not a PDF"), 0600); err != nil {
				t.Fatal(err)
			}
			plan["items"] = append(append([]any{}, items...), map[string]any{"source": "bad.pdf"})
		}
		data, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		report := filepath.Join(dir, fmt.Sprintf("report-%v.json", laterFailure))
		result, exit, _ := invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir, "--report", report}, string(data))
		wantExit := 0
		if laterFailure {
			wantExit = 2
		}
		if exit != wantExit {
			t.Fatalf("exit%d: %+v", exit, result.Diagnostics)
		}
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		validateResult(t, raw)
		var full exchange.Result
		if err = json.Unmarshal(raw, &full); err != nil {
			t.Fatal(err)
		}
		count := 0
		objects := map[int]bool{}
		for _, f := range full.Diagnostics {
			if f.Code.String() != "source_navigation_removed" {
				continue
			}
			count++
			if f.PDFLocation == nil || f.PDFLocation.Page != 2 || f.PDFLocation.Object < 8 || f.PDFLocation.Object > 32 || f.PDFLocation.Path != "Dest" || f.Detail == "" {
				t.Fatalf("lost source evidence: %+v", f)
			}
			objects[f.PDFLocation.Object] = true
			if len(f.Items) != 22 || f.ItemsOmitted != 0 {
				t.Fatalf("full consumer evidence: %+v", f)
			}
		}
		if count != 25 || len(objects) != 25 {
			t.Fatalf("distinct bindings collapsed: %d %v", count, objects)
		}
		if len(result.Diagnostics) != 20 || result.DiagnosticsOmitted != len(full.Diagnostics)-20 {
			t.Fatalf("diagnostic cap: summary%d omitted%d full%d", len(result.Diagnostics), result.DiagnosticsOmitted, len(full.Diagnostics))
		}
		for _, f := range result.Diagnostics {
			if f.Code.String() == "source_navigation_removed" {
				if len(f.Items) != 20 || f.ItemsOmitted != 2 || f.PDFLocation == nil {
					t.Fatalf("independent item cap lost evidence: %+v", f)
				}
			}
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"check", "--plan", "-", "--base-dir", dir, "--report", filepath.Join(dir, fmt.Sprintf("text-report-%v.json", laterFailure))}, strings.NewReader(string(data)), &stdout, &stderr)
		if code != wantExit || !strings.Contains(stderr.String(), "see the saved report") {
			t.Fatalf("saved-report omission hint: %d %q", code, stderr.String())
		}
	}
}

func TestRenamePredictionsUseOnlyRetainedOriginalSourceSites(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "named.pdf")
	if e := pdffixture.NamedDests("PREDICT").WriteFile(source); e != nil {
		t.Fatal(e)
	}
	input := `{"version":7,"items":[{"source":"named.pdf","pages":[[1,1]]},{"source":"named.pdf","pages":[[2,2]]},{"source":"named.pdf","pages":[[2,2]]}]}`
	r, exit, _ := invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir}, input)
	if exit != 0 {
		t.Fatal(r.Diagnostics)
	}
	count := 0
	for _, f := range r.Diagnostics {
		if f.Code.String() != "destinations_renamed" {
			continue
		}
		count++
		if !strings.HasPrefix(f.Message, "Predicted:") || !strings.Contains(f.Detail, "chap2") || !strings.Contains(f.Detail, "no actual") || len(f.Items) != 1 || f.Items[0] != "/items/2" || f.PDFLocation == nil || f.PDFLocation.Object != 6 || f.PDFLocation.Page != 0 || f.PDFLocation.Path != "Names[3]" {
			t.Fatalf("rename prediction invents/drops evidence: %+v", f)
		}
	}
	if count != 1 {
		t.Fatalf("retained destination predictions=%d", count)
	}
	form := filepath.Join(dir, "form.pdf")
	if e := pdffixture.Form("FORM").WriteFile(form); e != nil {
		t.Fatal(e)
	}
	r, exit, _ = invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir}, `{"version":7,"items":[{"source":"form.pdf"},{"source":"form.pdf"}]}`)
	if exit != 0 {
		t.Fatal(r.Diagnostics)
	}
	count = 0
	for _, f := range r.Diagnostics {
		if f.Code.String() != "form_fields_renamed" {
			continue
		}
		count++
		if !strings.HasPrefix(f.Message, "Predicted:") || !strings.Contains(f.Detail, "no actual") || f.PDFLocation == nil || f.PDFLocation.Object != 1 || f.PDFLocation.Page != 0 || f.PDFLocation.Path != "Catalog.AcroForm.Fields" || len(f.Items) != 1 || f.Items[0] != "/items/1" {
			t.Fatalf("field prediction: %+v", f)
		}
	}
	if count != 1 {
		t.Fatalf("field predictions=%d", count)
	}
}
