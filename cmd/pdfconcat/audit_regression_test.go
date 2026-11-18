// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestFittingUnitEquivalencePreservesPositionedBookmark(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	doc := pdffixture.WithBoxes("boundary", pdffixture.PageBoxes{}, pdffixture.PageBoxes{Media: "[0 0 73 100]"})
	doc.Objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R /Outlines 6 0 R >>")
	doc.Objs = append(doc.Objs, []byte("<< /Type /Outlines /First 7 0 R /Last 7 0 R /Count 1 >>"),
		[]byte("<< /Title (Positioned) /Parent 6 0 R /Dest [3 0 R /XYZ 5 50 null] >>"))
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	var original []byte
	for _, length := range []string{"1in", "25.4mm"} {
		out := filepath.Join(dir, length+".pdf")
		r, code, raw := invokeJSON(t, []string{"build", "--fit-to", "1x100", "--fit-tolerance", length, "-o", out, source}, "")
		if code != 0 || r.Fitting == nil || r.Fitting.Tolerance != 72 || !r.Output.Published {
			t.Fatalf("%s: %s", length, raw)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if original != nil && !bytes.Equal(original, data) {
			t.Fatal("equivalent fitting produced different output")
		}
		original = data
	}
}

func TestFittingCLIRejectsNegativeUnderflowAndShowsMetricGrammar(t *testing.T) {
	fitted := []string{"check", "--fit-to", "A4", "--fit-tolerance", "-0." + strings.Repeat("0", 340) + "1pt", "--blank"}
	r, code, raw := invokeJSON(t, fitted, "")
	if code != 2 || !hasDiagnostic(r, "usage_invalid_value") || r.Fitting != nil {
		t.Fatal(raw)
	}
	fitted[4] = "-0pt"
	r, code, raw = invokeJSON(t, fitted, "")
	if code != 0 || r.Fitting == nil || r.Fitting.Tolerance != 0 {
		t.Fatal(raw)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help", "build"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	const example = "--fit-to 210x297mm --fit-tolerance 0.5mm"
	if !strings.Contains(stdout.String(), example) || !strings.Contains(stdout.String(), "WIDTHxHEIGHT[unit]") {
		t.Fatal(stdout.String())
	}
	r, code, raw = invokeJSON(t, append([]string{"check", "--blank"}, strings.Fields(example)...), "")
	if code != 0 || r.Fitting == nil || r.Fitting.Tolerance < 1 {
		t.Fatal(raw)
	}
}

func TestURIBaseStagesRemainVisibleInJSONAndText(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "relative.pdf")
	if err := pdffixture.URILink("relative", "relative/path", "https://example.invalid/", false).WriteFile(source); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"check", "build"} {
		output := filepath.Join(dir, operation+".pdf")
		var out, stderr bytes.Buffer
		if code := run([]string{operation, source, "-o", output}, strings.NewReader(""), &out, &stderr); code != 0 {
			t.Fatal(stderr.String())
		}
		count := 1
		if operation == "build" {
			count = 2
		}
		if strings.Count(stderr.String(), "source_uri_base_removed") != count ||
			!strings.Contains(stderr.String(), "Inspection found") ||
			strings.Contains(stderr.String(), "Assembly found") != (count == 2) {
			t.Fatal(stderr.String())
		}
		r, code, raw := invokeJSON(t, []string{operation, source, "-o", output, "--overwrite"}, "")
		if code != 0 {
			t.Fatal(raw)
		}
		var details []string
		for _, d := range r.Diagnostics {
			if d.Code.String() == "source_uri_base_removed" {
				details = append(details, d.Detail)
			}
		}
		if len(details) != count || !strings.HasPrefix(details[0], "Inspection found") ||
			(count == 2 && !strings.HasPrefix(details[1], "Assembly found")) {
			t.Fatal(raw)
		}
	}
}

func TestNumberedGeneratedBlankAndPhysicalRefusal(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "blank.pdf")
	blank := `{"version":8,"page_numbers":{"margin":88,"size":9},"page_defaults":{"text":{"value":"Body"}},"items":[{"page":{"size":"100x100","text":{"value":""}}}]}`
	r, code, raw := invokeJSON(t, []string{"build", "--plan", "-", "-o", out}, blank)
	if code != 0 || !r.Output.Published {
		t.Fatal(raw)
	}
	body := strings.Replace(blank, `,"text":{"value":""}`, "", 1)
	r, code, raw = invokeJSON(t, []string{"check", "--plan", "-"}, body)
	if code != 2 || !hasDiagnostic(r, "text_overflow") || !strings.Contains(raw, "Page-number exclusion") {
		t.Fatal(raw)
	}
	plan, err := json.Marshal(map[string]any{"version": 8, "page_numbers": map[string]any{"margin": 100, "start": 19},
		"items": []map[string]any{{"source": out}}})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--plan", "-"}, bytes.NewReader(plan), &stdout, &stderr); code != 2 ||
		!strings.Contains(stderr.String(), "physical page 1") || !strings.Contains(stderr.String(), "Page number 19") ||
		strings.Contains(stderr.String(), "Generated text") || strings.Count(stderr.String(), "Decrease page_numbers.margin") != 1 {
		t.Fatal(stderr.String())
	}
	check, code, raw := invokeJSON(t, []string{"check", "--plan", "-"}, string(plan))
	if code != 2 || !hasDiagnostic(check, "text_overflow") || !strings.Contains(raw, "physical page 1") {
		t.Fatal(raw)
	}
	tools := oracle.RequireTools(t)
	pdf, err := oracle.Load(tools, out)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := pdf.RawTexts()
	if err != nil || len(pages) != 1 || strings.TrimSpace(pages[0]) != "1" {
		t.Fatalf("expected only a page number on the blank: %v %v", pages, err)
	}
}

func TestAllowedOverflowDistinguishesPhysicalInkAndWrapWidth(t *testing.T) {
	for _, tc := range []struct{ x, detail string }{{"0", "configured wrap width"}, {"-240", "Glyph ink crosses a physical page edge"}} {
		plan := `{"version":8,"items":[{"page":{"size":"300x400","text":{"value":"UnbrokenToken","size":16,"width":12,"x":` + tc.x + `,"overflow":"allow"}}}]}`
		r, code, raw := invokeJSON(t, []string{"check", "--plan", "-"}, plan)
		if code != 0 || !hasDiagnostic(r, "text_overflow_allowed") || !strings.Contains(raw, tc.detail) {
			t.Fatal(raw)
		}
	}
}
