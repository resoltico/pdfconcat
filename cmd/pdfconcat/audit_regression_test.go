// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
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
	blank := `{"version":11,"page_numbers":{"margin":88,"size":9},"page_defaults":{"text":{"value":"Body"}},"items":[{"page":{"size":"100x100","text":{"value":""}}}]}`
	r, code, raw := invokeJSON(t, []string{"build", "--plan", "-", "-o", out}, blank)
	if code != 0 || !r.Output.Published {
		t.Fatal(raw)
	}
	body := strings.Replace(blank, `,"text":{"value":""}`, "", 1)
	r, code, raw = invokeJSON(t, []string{"check", "--plan", "-"}, body)
	if code != 2 || !hasDiagnostic(r, "text_overflow") || !strings.Contains(raw, "Page-number exclusion") {
		t.Fatal(raw)
	}
	plan, err := json.Marshal(map[string]any{"version": 11, "page_numbers": map[string]any{"margin": 100, "start": 19},
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
		plan := `{"version":11,"items":[{"page":{"size":"300x400","text":{"value":"UnbrokenToken","size":16,"width":12,"x":` + tc.x + `,"overflow":"allow"}}}]}`
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
	plan := `{"version":11,"optional_content":"flatten-visible-content","items":[{"source":"prelude.pdf"}]}`
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

func TestZeroOffsetDiscardHintCheckAndBuild(t *testing.T) {
	dir := t.TempDir()
	source, out := filepath.Join(dir, "index.pdf"), filepath.Join(dir, "packet.pdf")
	doc := pdffixture.Plain("zero-offset structure index")
	doc.Objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R >>")
	doc.Objs = append(doc.Objs, []byte("<< /Type /StructTreeRoot /ParentTree 7 0 R >>"), []byte("null"))
	data := doc.Bytes()
	body := bytes.Index(data, []byte("7 0 obj\n"))
	row := fmt.Appendf(nil, "%010d 00000 n ", body)
	if body <= 0 || bytes.Count(data, row) != 1 {
		t.Fatal("missing independent xref row")
	}
	data = bytes.Replace(data, row, []byte("0000000000 00000 n "), 1)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{"direct", "preserve", "discard"} {
		t.Run(policy, func(t *testing.T) {
			const incumbent = "keep prior output"
			if err := os.WriteFile(out, []byte(incumbent), 0600); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"check", "build"} {
				args := []string{operation}
				var input string
				if policy == "direct" {
					args = append(args, source)
				} else {
					args = append(args, "--plan", "-", "--base-dir", dir)
					input = fmt.Sprintf(`{"version":11,"structure":%q,"items":[{"source":"index.pdf"}]}`, policy)
				}
				if operation == "build" {
					args = append(args, "--overwrite", "-o", out)
				}
				r, code, raw := invokeJSON(t, args, input)
				if policy == "discard" {
					if code != 0 || !hasDiagnostic(r, "source_structure_removed") {
						t.Fatal(raw)
					}
					found := false
					for _, d := range r.Diagnostics {
						if d.Code.String() == "source_xref_structure_reference_discarded" {
							found = d.PDFLocation != nil && d.PDFLocation.Object == 6 && d.PDFLocation.Path == "ParentTree" &&
								strings.Contains(d.Message, "discarded structure index referenced zero-offset object 7")
						}
					}
					if !found || operation == "build" && (r.Output == nil || !r.Output.Published) {
						t.Fatal(raw)
					}
					continue
				}
				if code != 2 || r.Status != "invalid" || len(r.Diagnostics) != 1 {
					t.Fatal(raw)
				}
				d := r.Diagnostics[0]
				if d.Code.String() != "source_invalid" || d.File != source || len(d.Items) != 1 || d.PDFLocation == nil ||
					d.PDFLocation.Object != 6 || d.PDFLocation.Path != "ParentTree" ||
					!strings.Contains(d.Message, `v11 plan with "structure": "discard"`) ||
					!strings.Contains(d.Message, "untagged output for the whole packet") ||
					!strings.Contains(d.Detail, "original reference to zero-offset object 7") {
					t.Fatal(raw)
				}
				if operation == "build" {
					if r.Output == nil || r.Output.Published {
						t.Fatal(raw)
					}
					if got, err := os.ReadFile(out); err != nil || string(got) != incumbent {
						t.Fatalf("preserve replaced the incumbent: %v", err)
					}
				}
			}
		})
	}
}

func TestRequiredFontBindingsUseOriginalDrawingScope(t *testing.T) {
	tools := oracle.RequireTools(t)
	const text = "BT /F1 14 Tf 20 50 Td (BOUND FONT) Tj ET"
	const fonts = "/Font << /F1 5 0 R >>"
	const otherFont = "/Font << /F1 7 0 R >>"
	const xobjects = "/XObject << /Art 6 0 R >> "
	const noForm = "-"
	const opaque = "/FMissing 10 Tf"
	for _, tc := range []struct {
		name, pageResources, formResources, drawing string
		missingOwner                                int
	}{
		{"missing page font", "", noForm, text, 3},
		{"inherited page font", fonts, noForm, text, 0},
		{"legacy Form borrows font", xobjects + fonts, "", "/Art Do", 0},
		{"explicit Form shadows page", xobjects + fonts, "<< >>", "/Art Do", 6},
		{"same name in separate scopes", xobjects + fonts, "<< " + otherFont + " >>", "q 1 0 0 1 0 40 cm /Art Do Q " + text, 0},
		{"opaque font lookalikes", fonts, noForm, "% " + opaque + "\n" + text + " BT /F1 10 Tf 20 30 Td (" + opaque +
			") Tj ET\n" + fmt.Sprintf("BI /W %d /H 1 /BPC 8 /CS /G ID %s EI", len(opaque), opaque), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source, output := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "packet.pdf")
			stream := func(entries, program string) []byte {
				return fmt.Appendf(nil, "<< /Length %d %s >>\nstream\n%s\nendstream", len(program), entries, program)
			}
			d := &pdffixture.Doc{Version: "1.7", Objs: [][]byte{
				[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
				fmt.Appendf(nil, "<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << %s >> >>", tc.pageResources),
				[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 220 160] /Contents 4 0 R >>"),
				stream("", tc.drawing),
				[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"),
			}}
			if tc.formResources != noForm {
				entries := "/Type /XObject /Subtype /Form /BBox [0 0 220 160]"
				if tc.formResources != "" {
					entries += " /Resources " + tc.formResources
				}
				d.Objs = append(d.Objs, stream(entries, text),
					[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>"))
			}
			if err := d.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			const incumbent = "prior output"
			for _, fit := range []bool{false, true} {
				if err := os.WriteFile(output, []byte(incumbent), 0600); err != nil {
					t.Fatal(err)
				}
				for _, op := range []string{"check", "build"} {
					args := []string{op, source}
					if fit {
						args = append(args, "--fit-to", "A4")
					}
					if op == "build" {
						args = append(args, "--overwrite", "-o", output)
					}
					r, code, raw := invokeJSON(t, args, "")
					if tc.missingOwner == 0 {
						if code != 0 || op == "build" && (r.Output == nil || !r.Output.Published) {
							t.Fatal(raw)
						}
						continue
					}
					if code != 2 || len(r.Diagnostics) != 1 {
						t.Fatal(raw)
					}
					finding := r.Diagnostics[0]
					loc := finding.PDFLocation
					if finding.Code.String() != "source_invalid" || finding.File != source || len(finding.Items) != 1 ||
						loc == nil || loc.Page != 1 || loc.Object != tc.missingOwner || loc.Path != "Content.Tf" {
						t.Fatal(raw)
					}
					got, err := os.ReadFile(output)
					if err != nil || string(got) != incumbent || r.Output != nil && r.Output.Published {
						t.Fatalf("font refusal replaced the incumbent: %q, %v", got, err)
					}
				}
				if tc.missingOwner == 0 {
					extracted, err := exec.CommandContext(t.Context(), tools.PDFToText, output, "-").Output()
					if err != nil || !bytes.Contains(extracted, []byte("BOUND FONT")) {
						t.Fatalf("output lost bound text: %q, %v", extracted, err)
					}
				}
			}
		})
	}
}

func TestDefaultSignatureRefusalOffersConditionalPlanGuidance(t *testing.T) {
	source := filepath.Join(t.TempDir(), "signed.pdf")
	if err := pdffixture.SignatureValue("signed").WriteFile(source); err != nil {
		t.Fatal(err)
	}
	plan := fmt.Sprintf(`{"version":11,"signatures":"refuse","items":[{"source":%q}]}`, filepath.ToSlash(source))
	for _, args := range [][]string{{"check", source}, {"check", "--plan", "-"}} {
		var stdout, stderr bytes.Buffer
		if exit := run(args, strings.NewReader(plan), &stdout, &stderr); exit != 2 {
			t.Fatalf("refusal exit %d: %s", exit, stderr.String())
		}
		for _, want := range []string{"source_signed", "PDF object 6", "deliver the original separately", "signatures", "discard", "pdfconcat check --plan FILE", "whether the source qualifies"} {
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("text refusal lacks %q: %s", want, stderr.String())
			}
		}
		result, exit, _ := invokeJSON(t, args, plan)
		if exit != 2 || len(result.Diagnostics) != 1 {
			t.Fatalf("structured refusal exit %d: %v", exit, result.Diagnostics)
		}
		d := result.Diagnostics[0]
		if d.Code.String() != "source_signed" || d.PDFLocation == nil || d.PDFLocation.Object != 6 ||
			d.PDFLocation.Path != "Object" || !strings.Contains(d.Detail, `"signatures": "discard"`) ||
			!strings.Contains(d.Detail, "whether the source qualifies") {
			t.Fatalf("structured refusal lost reason, guidance or location: %+v", d)
		}
	}
}

func TestEmptySignatureAppearanceReasonInTextAndJSON(t *testing.T) {
	source := filepath.Join(t.TempDir(), "printable.pdf")
	d := pdffixture.SignatureArtwork()
	d.Objs[5] = bytes.Replace(d.Objs[5], []byte("[75 85 175 145]"), []byte("[0 0 0 0]"), 1)
	if err := d.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	plan := fmt.Sprintf(`{"version":11,"signatures":"discard","items":[{"source":%q}]}`, filepath.ToSlash(source))
	args := []string{"check", "--plan", "-"}
	result, exit, _ := invokeJSON(t, args, plan)
	if exit != 2 || len(result.Diagnostics) != 1 {
		t.Fatalf("JSON refusal exit %d: %v", exit, result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if finding.Code.String() != "source_signature_discard_unsupported" ||
		!strings.Contains(finding.Message, "decoded appearance contains more than PDF whitespace and comments") ||
		finding.PDFLocation == nil || finding.PDFLocation.Object != 6 || finding.PDFLocation.Path != "Object.AP.N" ||
		!strings.Contains(finding.Detail, "PDF object 8 with generation 0") {
		t.Fatalf("JSON refusal lost its reason or appearance site: %+v", finding)
	}
	var text, stderr bytes.Buffer
	if exit := run(args, strings.NewReader(plan), &text, &stderr); exit != 2 {
		t.Fatalf("text refusal exit %d: %s", exit, stderr.String())
	}
	for _, want := range []string{finding.Message, finding.PDFLocation.Path, finding.Detail} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("text refusal lacks %q: %s", want, stderr.String())
		}
	}
}
