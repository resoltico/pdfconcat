// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func layerStream(entries, program string) []byte {
	return fmt.Appendf(nil, "<< %s /Length %d >>\nstream\n%s\nendstream", entries, len(program), program)
}

func layerFixture(properties, group, program string) *pdffixture.Doc {
	d := pdffixture.Plain("VISIBLE TEXT")
	if properties != "" {
		insertEntry(d, 1, "/OCProperties "+properties)
	}
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Font << /F1 5 0 R >>"), []byte("/Font << /F1 5 0 R >> /XObject << /Art 6 0 R >>"))
	d.Objs[3] = layerStream("", "BT /F1 14 Tf 80 500 Td (VISIBLE TEXT) Tj ET /Art Do")
	d.Objs = append(d.Objs,
		layerStream("/Type /XObject /Subtype /Form /BBox [0 0 600 700] /Resources << >> /OC 7 0 R", program),
		[]byte(group),
	)
	return d
}

func runLayerRequest(t *testing.T, dir string, op app.Operation, policy packet.OptionalContentPolicy, items ...packet.Item) app.Result {
	t.Helper()
	return app.Execute(t.Context(), app.Request{
		Operation: op, WorkingDir: dir, InspectionWorkers: 1, Overwrite: true,
		Output: filepath.Join(dir, "packet.pdf"), Report: filepath.Join(dir, string(op)+".json"),
		Input: app.Declared{Declaration: packet.Declaration{OptionalContent: policy, Items: items}},
	})
}

const layerGroup = "<< /Type /OCG /Name (Visible graphics) >>"
const layerColor = "1 0 0 rg 40 50 150 100 re f"
const layerCatalog = "<< /OCGs [7 0 R] /D << >> >>"

func TestReachableOrphanOptionalContentAtBothBoundaries(t *testing.T) {
	dir := t.TempDir()
	d := layerFixture("", layerGroup, layerColor)
	// Keep the affected page out of the selection. Admission still sees it.
	d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 8 0 R] >>")
	d.Objs = append(d.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> >>"))
	source := filepath.Join(dir, "layers.pdf")
	if err := d.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	qpdfCheck(t, source)
	if err := os.WriteFile(filepath.Join(dir, "packet.pdf"), []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []packet.OptionalContentPolicy{packet.OptionalContentRefuse, packet.OptionalContentFlattenVisibleForms} {
		for _, op := range []app.Operation{app.Check, app.Build} {
			result := runLayerRequest(t, dir, op, policy, &packet.SourceItem{Path: source, Selection: []packet.PageRange{{First: 2, Last: 2}}})
			if result.Status != "invalid" || result.Output.Published || !codePresent(result.Diagnostics, diag.SourceOptionalContent) {
				t.Fatal(result.Diagnostics)
			}
			for _, d := range result.Diagnostics {
				if d.Code == diag.SourceOptionalContent && strings.Contains(d.Message, "flatten-visible-forms") {
					t.Fatal(d)
				}
			}
			if got := readPDF(t, filepath.Join(dir, "packet.pdf")); string(got) != "sentinel" {
				t.Fatal("source refusal replaced the existing destination")
			}
			if report := readPDF(t, filepath.Join(dir, string(op)+".json")); !bytes.Contains(report, []byte("source_optional_content")) {
				t.Fatal(string(report))
			}
		}
	}
	if err := pdf.Verify(t.Context(), d.Bytes(), 2); err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid || pdf.Diagnostic(err).PDFLocation == nil {
		t.Fatalf("reopened verifier allowed a catalog-free layer: %v", err)
	}
	// Retain an unreachable OCG and harmless names/text in an otherwise verified PDF.
	form := strings.Replace(string(d.Objs[5]), " /OC 7 0 R", " /Private << /OC (OCG) >>", 1)
	d.Objs[5] = []byte(form)
	if err := pdf.Verify(t.Context(), d.Bytes(), 2); err != nil {
		t.Fatalf("independent control without the visibility edge: %v", err)
	}
}

func TestVisibleFormLayerAdmissionProfile(t *testing.T) {
	const usage = "/Usage << /View << /ViewState /ON >> /Print << /PrintState /ON >> /Export << /ExportState /ON >> /PageElement << /Subtype /BG >> >>"
	const applications = "/AS [<< /Event /View /Category [/View] /OCGs [7 0 R] >> << /Event /Print /Category [/Print] /OCGs [7 0 R] >> << /Event /Export /Category [/Export] /OCGs [7 0 R] >>]"
	for _, test := range []struct {
		name, config, usage, program string
		admit                        bool
	}{
		{"base-on-default", "", "", "", true},
		{"base-on-explicit", "/BaseState /ON /ON [7 0 R]", "", "", true},
		{"base-off-with-override", "/BaseState /OFF /ON [7 0 R]", "", "", false},
		{"empty-arrays", "/ON [] /OFF [] /Order [] /RBGroups [] /AS []", "", "", true},
		{"redundant-usage-applications", applications, "", "", true},
		{"stamp-background", applications, usage, "", true},
		{"invisible-base", "/BaseState /OFF", "", "", false},
		{"previous-state", "/BaseState /Unchanged", "", "", false},
		{"contradictory-off", "/ON [7 0 R] /OFF [7 0 R]", "", "", false},
		{"duplicate-on", "/ON [7 0 R 7 0 R]", "", "", false},
		{"order", "/Order [7 0 R]", "", "", false},
		{"radio", "/RBGroups [[7 0 R]]", "", "", false},
		{"unknown-configuration", "/Private true", "", "", false},
		{"unknown-usage", applications, strings.Replace(usage, "/View <<", "/Zoom << /min 2 >> /View <<", 1), "", false},
		{"different-print-state", applications, strings.Replace(usage, "/PrintState /ON", "/PrintState /OFF", 1), "", false},
		{"wrong-application", strings.Replace(applications, "/Category [/View]", "/Category [/Print]", 1), "", "", false},
		{"wrong-generation", "/ON [7 1 R]", "", "", false},
		{"optional-marked-content", "", "", "/OC /Name BDC " + layerColor + " EMC", false},
		{"malformed-marked-content", "", "", "/Span (not a property) BDC " + layerColor + " EMC", false},
		{"opaque-string-comment-and-image", "", "", "% /OC /Missing BDC\n/Span << /ActualText (/OC BDC) >> BDC " + layerColor + " EMC\nBI /W 7 /H 1 /BPC 8 /CS /G ID /OC BDC EI", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			properties := "<< /OCGs [7 0 R] /D << " + test.config + " >> >>"
			group := "<< /Type /OCG /Name (Visible graphics) " + test.usage + " >>"
			program := layerColor
			if test.program != "" {
				program = test.program
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "layer.pdf")
			d := layerFixture(properties, group, program)
			if err := d.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			// Graphics proof still belongs to the opt-in path.
			hint := test.admit || test.name == "optional-marked-content" || test.name == "malformed-marked-content"
			preview := runLayerRequest(t, dir, app.Check, packet.OptionalContentRefuse, &packet.SourceItem{Path: source})
			if len(preview.Diagnostics) != 1 || preview.Diagnostics[0].Code != diag.SourceOptionalContent ||
				strings.Contains(preview.Diagnostics[0].Message, "flatten-visible-forms") != hint {
				t.Fatal(preview.Diagnostics)
			}
			result := runLayerRequest(t, dir, app.Check, packet.OptionalContentFlattenVisibleForms, &packet.SourceItem{Path: source})
			if test.admit {
				if result.Status != "ok" || !codePresent(result.Diagnostics, diag.SourceOptionalContentFlattened) {
					t.Fatal(result.Diagnostics)
				}
				return
			}
			if result.Status != "invalid" || !codePresent(result.Diagnostics, diag.SourceOptionalContent) {
				t.Fatal(result.Diagnostics)
			}
			for _, d := range result.Diagnostics {
				if d.Code == diag.SourceOptionalContent && strings.Contains(d.Message, "flatten-visible-forms") {
					t.Fatal(d)
				}
			}
		})
	}
}

func TestDefaultLayerRefusalWithholdsHintForUnsupportedStaticProfile(t *testing.T) {
	for _, tc := range []struct {
		name, properties, group string
	}{
		{"empty", "<< /OCGs [] /D << >> >>", layerGroup},
		{"non-array", "<< /OCGs 17 /D << >> >>", layerGroup},
		{"multiple", "<< /OCGs [7 0 R 8 0 R] /D << >> >>", layerGroup},
		{"wrong-group-type", layerCatalog, "<< /Type /Other /Name (Wrong type) >>"},
		{"alternate-configuration", "<< /OCGs [7 0 R] /D << >> /Configs [<< /BaseState /OFF >>] >>", layerGroup},
		{"missing-default", "<< /OCGs [7 0 R] >>", layerGroup},
		{"missing-name", layerCatalog, "<< /Type /OCG >>"},
		{"non-text-name", layerCatalog, "<< /Type /OCG /Name 42 >>"},
		{"oversized-applications", "<< /OCGs [7 0 R] /D << /AS [" + strings.Repeat("null ", 4096) + "] >> >>", layerGroup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "layers.pdf")
			doc := layerFixture(tc.properties, tc.group, layerColor)
			doc.Objs = append(doc.Objs, []byte("<< /Type /OCG /Name (Other group) >>"))
			if err := doc.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			for _, policy := range []packet.OptionalContentPolicy{packet.OptionalContentRefuse, packet.OptionalContentFlattenVisibleForms} {
				result := runLayerRequest(t, dir, app.Check, policy, &packet.SourceItem{Path: source})
				if result.Status != "invalid" || len(result.Diagnostics) != 1 ||
					result.Diagnostics[0].Code != diag.SourceOptionalContent || strings.Contains(result.Diagnostics[0].Message, "flatten-visible-forms") {
					t.Fatal(result.Diagnostics)
				}
				if policy == packet.OptionalContentRefuse && (!strings.Contains(result.Diagnostics[0].Message, "visibility layers") ||
					!strings.Contains(result.Diagnostics[0].Message, "original separately")) {
					t.Fatal(result.Diagnostics)
				}
			}
		})
	}
}

func TestVisibleFormLayerRefusesOtherReachableUses(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*pdffixture.Doc)
	}{
		{"extra-group-same-name", func(d *pdffixture.Doc) {
			d.Objs[0] = bytes.ReplaceAll(d.Objs[0], []byte("[7 0 R]"), []byte("[7 0 R 8 0 R]"))
			d.Objs = append(d.Objs, []byte(layerGroup))
		}},
		{"alternate-config", func(d *pdffixture.Doc) {
			d.Objs[0] = bytes.ReplaceAll(d.Objs[0], []byte("/D <<"), []byte("/Configs [<< /BaseState /OFF >>] /D <<"))
		}},
		{"typed-membership-expression", func(d *pdffixture.Doc) {
			insertEntry(d, 3, "/Other << /Type /OCMD /OCGs [7 0 R] /VE [/Not 7 0 R] >>")
		}},
		{"property-list", func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/XObject"), []byte("/Properties << /Layer 7 0 R >> /XObject"))
		}},
		{"annotation", func(d *pdffixture.Doc) {
			insertEntry(d, 3, "/Annots [8 0 R]")
			d.Objs = append(d.Objs, []byte("<< /Subtype /Link /Rect [10 20 30 40] /OC 7 0 R >>"))
		}},
		{"alternate-image", func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Art 6 0 R"), []byte("/Art 6 0 R /Image 8 0 R"))
			d.Objs = append(d.Objs, layerStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Alternates [<< /Image 9 0 R /OC 7 0 R >>]", "G"), layerStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "X"))
		}},
		{"state-action-chain", func(d *pdffixture.Doc) {
			insertEntry(d, 1, "/OpenAction << /S /Named /N /FirstPage /Next << /S /SetOCGState /State [/Toggle 7 0 R] >> >>")
		}},
		{"javascript", func(d *pdffixture.Doc) {
			insertEntry(d, 1, "/Names << /JavaScript << /Names [(script) << /S /JavaScript /JS (app.alert\\(1\\)) >>] >> >>")
		}},
		{"reference-xobject", func(d *pdffixture.Doc) {
			d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/OC"), []byte("/Ref << /F (external.pdf) /Page 0 >> /OC"))
		}},
		{"unclassified-form-reference", func(d *pdffixture.Doc) {
			insertEntry(d, 3, "/Private 6 0 R")
		}},
		{"marked-content-across-streams", func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Contents 4 0 R"), []byte("/Contents [4 0 R 8 0 R]"))
			d.Objs[3] = layerStream("", "/OC")
			d.Objs = append(d.Objs, layerStream("", "/Hidden BDC /Art Do EMC"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := layerFixture(layerCatalog, layerGroup, layerColor)
			test.mutate(d)
			dir := t.TempDir()
			source := filepath.Join(dir, "layer.pdf")
			if err := d.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			result := runLayerRequest(t, dir, app.Build, packet.OptionalContentFlattenVisibleForms, &packet.SourceItem{Path: source})
			if result.Status != "invalid" || result.Output.Published || !codePresent(result.Diagnostics, diag.SourceOptionalContent) {
				t.Fatal(result.Diagnostics)
			}
		})
	}
}

func TestPdfcpuStampFlattensWithVisibleArtworkAndRepeatedSources(t *testing.T) {
	dir := t.TempDir()
	input, source := filepath.Join(dir, "input.pdf"), filepath.Join(dir, "stamp.pdf")
	if err := pdffixture.Tagged("TAGGED SOURCE").WriteFile(input); err != nil {
		t.Fatal(err)
	}
	if err := api.AddTextWatermarksFile(t.Context(), input, source, nil, true, "SAMPLE", "pos:c, rot:0", model.NewStatelessConfiguration()); err != nil {
		t.Fatal(err)
	}
	qpdfCheck(t, source)
	original := sha256.Sum256(readPDF(t, source))
	first := &packet.SourceItem{Ref: "first", Path: source}
	last := &packet.SourceItem{Ref: "last", Path: source, Selection: []packet.PageRange{{First: 1, Last: 1}}}
	defaultPolicy := runLayerRequest(t, dir, app.Check, packet.OptionalContentRefuse, first)
	if len(defaultPolicy.Diagnostics) != 1 || defaultPolicy.Diagnostics[0].Code != diag.SourceOptionalContent ||
		!strings.Contains(defaultPolicy.Diagnostics[0].Message, `"optional_content": "flatten-visible-forms"`) ||
		!strings.Contains(defaultPolicy.Diagnostics[0].Message, "If the source qualifies") ||
		!strings.Contains(defaultPolicy.Diagnostics[0].Message, "Deliver the original separately") {
		t.Fatal(defaultPolicy.Diagnostics)
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		result := runLayerRequest(t, dir, op, packet.OptionalContentFlattenVisibleForms, first, &packet.PageItem{Count: 1}, last)
		if result.Status != "ok" || result.Output.Published != (op == app.Build) || len(result.Parts) != 3 {
			t.Fatal(result.Diagnostics)
		}
		warnings := 0
		for _, d := range result.Diagnostics {
			if d.Code == diag.SourceOptionalContentFlattened {
				warnings++
				if len(d.Items) != 2 || d.PDFLocation == nil || d.PDFLocation.Object == 0 ||
					!strings.Contains(d.Detail, "Watermark") || !strings.Contains(d.Message, "composition plan") {
					t.Fatal(d)
				}
			}
		}
		if warnings != 1 || result.Parts[0].SHA256 != hex.EncodeToString(original[:]) {
			t.Fatal("expected a warning per source and the original digest", result)
		}
	}
	out := filepath.Join(dir, "packet.pdf")
	qpdfCheck(t, out)
	tools := oracle.RequireTools(t)
	before, err := oracle.Load(tools, source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := oracle.Load(tools, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []int{1, 3} {
		delta, e := after.AppearanceDifference(page, before, 1, 0)
		if e != nil || delta != 0 {
			t.Fatal("stamp or original artwork shifted", delta, e)
		}
		text := string(run(t, "pdftotext", "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), out, "-"))
		if !strings.Contains(text, "SAMPLE") || !strings.Contains(text, "TAGGED SOURCE") {
			t.Fatal(text)
		}
	}
	for _, d := range qpdfDictionaries(t, out) {
		if d["/OC"] != nil || d["/OCProperties"] != nil || d["/Type"] == "/OCG" {
			t.Fatal("flattened layer survived", d)
		}
	}
	if got := sha256.Sum256(readPDF(t, source)); got != original {
		t.Fatal("source bytes changed")
	}
}

func TestLayerAndDestinationFindingsDescribePlansWhenBuildCannotPublish(t *testing.T) {
	dir := t.TempDir()
	layer, nav := filepath.Join(dir, "layer.pdf"), filepath.Join(dir, "nav.pdf")
	if err := layerFixture(layerCatalog, layerGroup, layerColor).WriteFile(layer); err != nil {
		t.Fatal(err)
	}
	if err := landscapeDestination("/XYZ 40 300 0", 0, false).WriteFile(nav); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "output.pdf")
	var preview map[diag.Code]diag.Diagnostic
	for _, op := range []app.Operation{app.Check, app.Build} {
		items := []packet.Item{
			&packet.SourceItem{Ref: "layer", Path: layer},
			&packet.SourceItem{Ref: "nav", Path: nav},
		}
		if op == app.Build {
			if err := os.WriteFile(out, []byte("existing output"), 0600); err != nil {
				t.Fatal(err)
			}
			style := packet.Defaults()
			style.Text.Value = packet.Setting[string]{Present: true, Value: strings.Repeat("W", 200)}
			style.Text.Width = packet.Setting[float64]{Present: true, Value: 40}
			items = append(items, &packet.PageItem{Count: 1, Style: style})
		}
		result := app.Execute(t.Context(), app.Request{Operation: op, WorkingDir: dir, Output: out,
			InspectionWorkers: 1, Overwrite: true, Fitting: packet.DefaultFitPolicy(packet.Size{Width: 400, Height: 600}),
			Input: app.Declared{Declaration: packet.Declaration{OptionalContent: packet.OptionalContentFlattenVisibleForms, Items: items}}})
		if result.Output.Published || op == app.Check && result.Status != "ok" ||
			op == app.Build && (result.Status != "invalid" || !codePresent(result.Diagnostics, diag.TextOverflow)) {
			t.Fatal(result)
		}
		findings := make(map[diag.Code]diag.Diagnostic)
		for _, d := range result.Diagnostics {
			switch d.Code {
			case diag.SourceOptionalContentFlattened, diag.FitDestinationMapped:
				findings[d.Code] = d
			}
		}
		for code, source := range map[diag.Code]string{diag.SourceOptionalContentFlattened: layer, diag.FitDestinationMapped: nav} {
			d := findings[code]
			if d.File != source || d.PDFLocation == nil || d.Detail == "" || !strings.Contains(d.Message, " plan ") {
				t.Fatal(d)
			}
			if op == app.Build && (d.Message != preview[code].Message || d.Detail != preview[code].Detail) {
				t.Fatal(d, preview[code])
			}
		}
		if op == app.Check {
			preview = findings
		} else if string(readPDF(t, out)) != "existing output" {
			t.Fatal("later refusal replaced the existing output")
		}
	}
}

func TestLayerWarningQualifiesArtworkByPageSelection(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "two-pages.pdf")
	doc := layerFixture(layerCatalog, layerGroup, layerColor)
	doc.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 8 0 R] >>")
	doc.Objs = append(doc.Objs, doc.Objs[2], layerStream("", "20 20 10 10 re f"))
	doc.Objs[2] = []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 9 0 R >>")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	for _, page := range []int{1, 2} {
		item := &packet.SourceItem{Path: source, Selection: []packet.PageRange{{First: page, Last: page}}}
		for _, op := range []app.Operation{app.Check, app.Build} {
			result := runLayerRequest(t, dir, op, packet.OptionalContentFlattenVisibleForms, item)
			if result.Status != "ok" || result.Output.Published != (op == app.Build) {
				t.Fatal(result.Diagnostics)
			}
			warnings := 0
			for _, d := range result.Diagnostics {
				if d.Code == diag.SourceOptionalContentFlattened {
					warnings++
					if d.File != source || d.PDFLocation == nil || d.PDFLocation.Object != 7 ||
						!strings.Contains(d.Detail, "Visible graphics") || !strings.Contains(d.Message, "retained after page selection") {
						t.Fatal(d)
					}
				}
			}
			if warnings != 1 {
				t.Fatal(result.Diagnostics)
			}
		}
		output := filepath.Join(dir, "packet.pdf")
		if pages := qpdfCheck(t, output); pages != 1 {
			t.Fatal(pages)
		}
		qdf := run(t, qpdfTool, "--qdf", "--object-streams=disable", output, "-")
		if bytes.Contains(qdf, []byte(layerColor)) != (page == 2) {
			t.Fatalf("unexpected Form artwork when selecting page %d", page)
		}
	}
}

func TestLayerPolicyKeepsSignatureAndDamageRefusals(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*pdffixture.Doc)
		code diag.Code
	}{
		{"signature", func(d *pdffixture.Doc) {
			insertEntry(d, 1, "/Perms << /DocMDP 8 0 R >>")
			d.Objs = append(d.Objs, []byte("<< /Type /Sig >>"))
		}, diag.SourceSigned},
		{"wrong-content-generation", func(d *pdffixture.Doc) {
			d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Contents 4 0 R"), []byte("/Contents 4 1 R"))
		}, diag.SourceDamaged},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := layerFixture(layerCatalog, layerGroup, layerColor)
			test.edit(d)
			dir := t.TempDir()
			src := filepath.Join(dir, "source.pdf")
			if err := d.WriteFile(src); err != nil {
				t.Fatal(err)
			}
			for _, policy := range []packet.OptionalContentPolicy{packet.OptionalContentRefuse, packet.OptionalContentFlattenVisibleForms} {
				result := runLayerRequest(t, dir, app.Build, policy, &packet.SourceItem{Path: src})
				if result.Status != "invalid" || result.Output.Published || !codePresent(result.Diagnostics, test.code) {
					t.Fatal(result.Diagnostics)
				}
			}
		})
	}
}

func TestNullOptionalContentSlotsAreAbsent(t *testing.T) {
	for _, value := range []string{"null", "7 0 R"} {
		t.Run(value, func(t *testing.T) {
			d := layerFixture("", "null", layerColor)
			insertEntry(d, 1, "/OCProperties "+value)
			d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/OC 7 0 R"), []byte("/OC "+value))
			d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Art 6 0 R"), []byte("/Art 6 0 R /Image 8 0 R"))
			insertEntry(d, 3, "/Annots [9 0 R]")
			const image = "/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"
			d.Objs = append(d.Objs,
				layerStream(image+" /OC "+value, "G"),
				[]byte("<< /Type /Annot /Subtype /Link /Rect [10 20 30 40] /OC "+value+" >>"),
			)
			if err := pdf.Verify(t.Context(), d.Bytes(), 1); err != nil {
				t.Fatalf("reopened output with absent slots: %+v", pdf.Diagnostic(err))
			}
			dir := t.TempDir()
			source := filepath.Join(dir, "layer.pdf")
			if err := d.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			for _, policy := range []packet.OptionalContentPolicy{packet.OptionalContentRefuse, packet.OptionalContentFlattenVisibleForms} {
				for _, op := range []app.Operation{app.Check, app.Build} {
					result := runLayerRequest(t, dir, op, policy, &packet.SourceItem{Path: source})
					if result.Status != "ok" || result.Output.Published != (op == app.Build) ||
						codePresent(result.Diagnostics, diag.SourceOptionalContentFlattened) {
						t.Fatal(result.Diagnostics)
					}
				}
			}
			qpdfCheck(t, filepath.Join(dir, "packet.pdf"))
		})
	}
}

func TestNullSlotsDuringVisibleLayerFlattening(t *testing.T) {
	d := layerFixture("<< /OCGs [7 0 R] /D << /BaseState 8 0 R /ON 8 0 R >> >>", layerGroup, layerColor)
	d.Objs = append(d.Objs, []byte("null"), []byte("<< /Type /Annot /Subtype /Link /Rect [10 20 30 40] /OC 8 0 R >>"))
	insertEntry(d, 3, "/Annots [9 0 R]")
	dir := t.TempDir()
	source := filepath.Join(dir, "layer.pdf")
	if err := d.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		result := runLayerRequest(t, dir, op, packet.OptionalContentFlattenVisibleForms, &packet.SourceItem{Path: source})
		if result.Status != "ok" || !codePresent(result.Diagnostics, diag.SourceOptionalContentFlattened) {
			t.Fatal(result.Diagnostics)
		}
	}
}

func TestLayerNullHandlingPreservesReferenceIntegrity(t *testing.T) {
	for _, bad := range []string{"7 1 R", "8 0 R", "true", "<< >>"} {
		for _, role := range []string{"catalog", "form"} {
			t.Run(role+"/"+bad, func(t *testing.T) {
				d := layerFixture("", "null", layerColor)
				if role == "catalog" {
					insertEntry(d, 1, "/OCProperties "+bad)
					d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/OC 7 0 R"), nil)
				} else {
					d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/OC 7 0 R"), []byte("/OC "+bad))
				}
				_, _, ds := pdf.Inspect(t.Context(), d.Bytes(), "", nil)
				if !codePresent(ds, diag.SourceOptionalContent) {
					t.Fatal(ds)
				}
				if err := pdf.Verify(t.Context(), d.Bytes(), 1); err == nil || pdf.Diagnostic(err).Code != diag.OutputInvalid {
					t.Fatalf("output guard accepted malformed layer value: %v", err)
				}
			})
		}
	}
}

func TestAlternateImageNullDoesNotTriggerLayerRefusal(t *testing.T) {
	for _, value := range []string{"null", "7 0 R"} {
		t.Run(value, func(t *testing.T) {
			d := layerFixture("", "null", layerColor)
			d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/OC 7 0 R"), nil)
			d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Art 6 0 R"), []byte("/Art 6 0 R /Image 8 0 R"))
			const image = "/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8"
			d.Objs = append(d.Objs,
				layerStream(image+" /Alternates [<< /Image 9 0 R /OC "+value+" >>]", "G"),
				layerStream(image, "X"),
			)
			// The pinned validator handles the alternate-image dictionary
			// after the raw layer boundary. Its separate policy still applies.
			_, _, ds := pdf.Inspect(t.Context(), d.Bytes(), "", nil)
			if codePresent(ds, diag.SourceOptionalContent) {
				t.Fatal(ds)
			}
		})
	}
}
