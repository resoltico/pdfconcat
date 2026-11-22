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

func TestURIBaseWarningUsesEstablishedStageInJSONAndText(t *testing.T) {
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
		phase := "Inspection found"
		if operation == "build" {
			phase = "Assembly found"
		}
		if strings.Count(stderr.String(), "source_uri_base_removed") != 1 || !strings.Contains(stderr.String(), phase) {
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
		if len(details) != 1 || !strings.HasPrefix(details[0], phase) {
			t.Fatal(raw)
		}
	}
}

func TestNumberedGeneratedBlankAndPhysicalRefusal(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "blank.pdf")
	blank := `{"version":10,"page_numbers":{"margin":88,"size":9},"page_defaults":{"text":{"value":"Body"}},"items":[{"page":{"size":"100x100","text":{"value":""}}}]}`
	r, code, raw := invokeJSON(t, []string{"build", "--plan", "-", "-o", out}, blank)
	if code != 0 || !r.Output.Published {
		t.Fatal(raw)
	}
	body := strings.Replace(blank, `,"text":{"value":""}`, "", 1)
	r, code, raw = invokeJSON(t, []string{"check", "--plan", "-"}, body)
	if code != 2 || !hasDiagnostic(r, "text_overflow") || !strings.Contains(raw, "Page-number exclusion") {
		t.Fatal(raw)
	}
	plan, err := json.Marshal(map[string]any{"version": 10, "page_numbers": map[string]any{"margin": 100, "start": 19},
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
		plan := `{"version":10,"items":[{"page":{"size":"300x400","text":{"value":"UnbrokenToken","size":16,"width":12,"x":` + tc.x + `,"overflow":"allow"}}}]}`
		r, code, raw := invokeJSON(t, []string{"check", "--plan", "-"}, plan)
		if code != 0 || !hasDiagnostic(r, "text_overflow_allowed") || !strings.Contains(raw, tc.detail) {
			t.Fatal(raw)
		}
	}
}

func TestMissingMarkedContentPropertyRefusedWithSourceAttribution(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "missing.pdf")
	for _, op := range []string{"BDC", "DP"} {
		doc := pdffixture.EncodedPages("missing property", []pdffixture.EncodedStream{{Data: []byte("/Span /Missing " + op + "\n0 0 10 10 re f")}})
		if op == "BDC" {
			doc = pdffixture.EncodedPages("missing property", []pdffixture.EncodedStream{{Data: []byte("/Span /Missing BDC\n0 0 10 10 re f\nEMC")}})
		}
		if err := doc.WriteFile(source); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, "packet.pdf")
		for _, operation := range []string{"check", "build"} {
			args := []string{operation, source}
			if operation == "build" {
				args = append(args, "-o", out)
			}
			result, code, raw := invokeJSON(t, args, "")
			if code != 2 || len(result.Diagnostics) != 1 {
				t.Fatalf("%s %s: %s", op, operation, raw)
			}
			d := result.Diagnostics[0]
			if d.Code.String() != "source_invalid" || d.File != source || d.PDFLocation == nil ||
				d.PDFLocation.Page != 1 || d.PDFLocation.Path != "Content."+op || len(d.Items) != 1 || d.Items[0] != "argv:1" {
				t.Fatalf("%s %s: %s", op, operation, raw)
			}
		}
	}
}

func TestWrapperPreludeRefusalIncludesResolvedReasonAndLocation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "prelude.pdf")
	const drawing = "1 0 0 1 0 0 cm\n/GS gs\n/OC /Layer BDC\n0 0 10 10 re f\nEMC"
	doc := pdffixture.EncodedPages("wrapper prelude", []pdffixture.EncodedStream{{Data: []byte(drawing)}})
	doc.Objs[3] = bytes.Replace(doc.Objs[3], []byte("/Font"), []byte("/Properties << /Layer << /Type /OCG /Name (Layer) >> >> /Font"), 1)
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	plan := `{"version":10,"optional_content":"flatten-visible-content","items":[{"source":"prelude.pdf"}]}`
	result, code, raw := invokeJSON(t, []string{"check", "--plan", "-", "--base-dir", dir}, plan)
	if code != 2 || len(result.Diagnostics) != 1 {
		t.Fatal(raw)
	}
	d := result.Diagnostics[0]
	if d.Code.String() != "source_optional_content" || d.File != source || d.PDFLocation == nil ||
		d.PDFLocation.Page != 1 || d.PDFLocation.Path != "Contents" ||
		!strings.Contains(d.Message, `"gs" at decoded byte 19`) ||
		!strings.Contains(d.Message, "resource could not be resolved") {
		t.Fatal(raw)
	}
}
