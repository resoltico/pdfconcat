// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func codePresent(ds []diag.Diagnostic, code diag.Code) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}
func insertEntry(doc *pdffixture.Doc, object int, entry string) *pdffixture.Doc {
	raw := doc.Objs[object-1]
	last := bytes.LastIndex(raw, []byte(">>"))
	if last < 0 {
		panic("not a dictionary")
	}
	doc.Objs[object-1] = append(append(append([]byte{}, raw[:last]...), []byte(" "+entry+" ")...), raw[last:]...)
	return doc
}

func TestInspectionPolicy(t *testing.T) {
	cases := []struct {
		name string
		doc  func() *pdffixture.Doc
		code diag.Code
	}{
		{"signed", func() *pdffixture.Doc { return pdffixture.SignatureValue("signed") }, diag.SourceSigned},
		{"perms", func() *pdffixture.Doc { return insertEntry(pdffixture.Plain("certified"), 1, "/Perms << >>") }, diag.SourceSigned},
		{"optional", func() *pdffixture.Doc {
			return insertEntry(pdffixture.Plain("oc"), 1, "/OCProperties << /OCGs [] /D << /Order [] >> >>")
		}, diag.SourceOptionalContent},
		{"dynamic", func() *pdffixture.Doc { return insertEntry(pdffixture.Plain("xfa"), 1, "/NeedsRendering true") }, diag.SourceDynamicForm},
		{"page kids", func() *pdffixture.Doc { return insertEntry(pdffixture.Plain("page"), 3, "/Kids []") }, diag.SourceDamaged},
		{"page child", func() *pdffixture.Doc { return insertEntry(pdffixture.Plain("page"), 3, "/Kids [3 0 R]") }, diag.SourceDamaged},
		{"empty kid", func() *pdffixture.Doc { d := pdffixture.Plain("page"); d.Objs[2] = []byte("<< >>"); return d }, diag.SourceDamaged},
		{"missing kid", func() *pdffixture.Doc {
			d := pdffixture.Plain("page")
			d.Objs[1] = []byte("<< /Type /Pages /Kids [99 0 R] /Count 1 >>")
			return d
		}, diag.SourceDamaged},
		{"wrong count", func() *pdffixture.Doc {
			d := pdffixture.Plain("page")
			d.Objs[1] = bytes.ReplaceAll(d.Objs[1], []byte("/Count 1"), []byte("/Count 2"))
			return d
		}, diag.SourceDamaged},
		{"private hash crop", func() *pdffixture.Doc {
			return insertEntry(pdffixture.Plain("visible"), 3, "/Cr#236fpBox [20 20 80 80]")
		}, diag.SourceInvalid},
		{"tagged", func() *pdffixture.Doc { return pdffixture.Tagged("tag") }, diag.SourceStructureRemoved},
		{"attachment", func() *pdffixture.Doc { return pdffixture.Attachment("file") }, diag.SourceAttachmentsRemoved},
		{"page labels", func() *pdffixture.Doc {
			return insertEntry(pdffixture.Plain("labels"), 1, "/PageLabels << /Nums [0 << /S /D >>] >>")
		}, diag.SourcePageLabelsRemoved},
		{"open action", func() *pdffixture.Doc { return insertEntry(pdffixture.Plain("open"), 1, "/OpenAction [3 0 R /Fit]") }, diag.SourceDocumentActionsRemoved},
		{"output intent", func() *pdffixture.Doc {
			return insertEntry(pdffixture.Plain("intent"), 1, "/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (test) >>]")
		}, diag.SourceOutputIntentsRemoved},
		{"relative uri", func() *pdffixture.Doc {
			return pdffixture.URILink("uri", "relative", "https://example.invalid/", false)
		}, diag.SourceURIBaseRemoved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ds := pdf.Inspect(t.Context(), tc.doc().Bytes())
			if !codePresent(ds, tc.code) {
				t.Fatalf("want %s, got %+v", tc.code, ds)
			}
		})
	}
	for name, doc := range map[string]*pdffixture.Doc{
		"plain": pdffixture.Plain("plain"), "null Kids": insertEntry(pdffixture.Plain("null"), 3, "/Kids null"),
		"unsigned signature":    pdffixture.EmptySignatureField("unsigned"),
		"ordinary literal hash": insertEntry(pdffixture.Plain("hash"), 3, "/unfinished#23 (private)"),
		"absolute URI":          pdffixture.URILink("uri", "https://example.invalid/x", "https://example.invalid/base/", false),
	} {
		t.Run(name, func(t *testing.T) {
			facts, ds := pdf.Inspect(t.Context(), doc.Bytes())
			status, _ := diag.Outcome(ds)
			if status != "ok" || facts.Pages != 1 || codePresent(ds, diag.SourceURIBaseRemoved) {
				t.Fatalf("facts %+v diagnostics %+v", facts, ds)
			}
		})
	}
	hostile, err := os.ReadFile("testdata/malformed-form-fields-panic.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, ds := pdf.Inspect(t.Context(), hostile); !codePresent(ds, diag.SourceDamaged) {
		t.Fatalf("raw hostile refusal: %+v", ds)
	}
}

func TestEncryptedSources(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, password := range []string{"", "secret"} {
		path := filepath.Join(t.TempDir(), "encrypted.pdf")
		if err := pdffixture.Plain("encrypted").WriteFile(path); err != nil {
			t.Fatal(err)
		}
		if err := tools.Encrypt(path, password); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, ds := pdf.Inspect(t.Context(), data); !codePresent(ds, diag.SourceEncrypted) {
			t.Fatalf("password %q: %+v", password, ds)
		}
	}
}

func assembleDocs(t *testing.T, docs []*pdffixture.Doc, fit *packet.Size) (string, pdf.Assembly, []diag.Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	j := packet.NewPlan("", fit, nil, packet.PageStyle{}, nil)
	facts := map[string]pdf.SourceFacts{}
	domain := map[string]packet.SourceFacts{}
	var warnings []diag.Diagnostic
	for i, d := range docs {
		path := filepath.Join(dir, fmt.Sprintf("source-%d.pdf", i))
		if err := d.WriteFile(path); err != nil {
			t.Fatal(err)
		}
		f, ds := pdf.Inspect(t.Context(), d.Bytes())
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatalf("inspect %d: %+v", i, ds)
		}
		warnings = append(warnings, ds...)
		facts[path] = f
		domain[fmt.Sprintf("/items/%d", i)] = f.SourceFacts
		j.Items = append(j.Items, &packet.SourceItem{Ref: fmt.Sprintf("/items/%d", i), Path: path})
	}
	layout, ds := packet.Resolve(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), domain, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	path := filepath.Join(dir, "output.pdf")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pdf.Assemble(t.Context(), layout, facts, wholeOccurrences(layout, facts), nil, fit, nil, nil, f)
	closeErr := f.Close()
	if err != nil {
		return path, a, []diag.Diagnostic{pdf.Diagnostic(err)}
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if err = pdf.Verify(t.Context(), path, layout.TotalPages); err != nil {
		t.Fatalf("verify: %v", err)
	}
	tools := oracle.RequireTools(t)
	if err = tools.CheckLinks(path); err != nil {
		t.Fatal(err)
	}
	return path, a, append(warnings, a.Diagnostics...)
}

func TestMergedDestinationsOutlinesAndForms(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, tc := range []struct {
		name string
		docs []*pdffixture.Doc
	}{
		{"named repeated", []*pdffixture.Doc{pdffixture.Plain("start"), pdffixture.NamedDests("N"), pdffixture.NamedDests("N"), pdffixture.NamedDests("N")}},
		{"named distinct", []*pdffixture.Doc{pdffixture.Plain("start"), pdffixture.NamedDests("A"), renameNames(pdffixture.NamedDests("B"))}},
		{"legacy repeated", []*pdffixture.Doc{pdffixture.Plain("start"), pdffixture.LegacyDests("L"), pdffixture.LegacyDests("L")}},
		{"outlines", []*pdffixture.Doc{pdffixture.Plain("start"), pdffixture.Outlined("O"), pdffixture.Outlined("O")}},
		{"forms", []*pdffixture.Doc{pdffixture.Form("F"), pdffixture.Form("F")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, a, ds := assembleDocs(t, tc.docs, nil)
			if status, _ := diag.Outcome(ds); status != "ok" {
				t.Fatal(ds)
			}
			if err := tools.Check(path); err != nil {
				t.Fatal(err)
			}
			doc, err := oracle.Load(tools, path)
			if err != nil {
				t.Fatal(err)
			}
			exp := oracle.Expectation{Sources: map[string]oracle.SourceFact{}}
			for i := range tc.docs {
				key := fmt.Sprintf("source-%d", i)
				source, e := oracle.Load(tools, filepath.Join(filepath.Dir(path), key+".pdf"))
				if e != nil {
					t.Fatal(e)
				}
				fact := oracle.SourceFact{Pages: source.PageCount(), Version: source.EffectiveVersion()}
				if strings.HasPrefix(tc.name, "named") || strings.HasPrefix(tc.name, "legacy") {
					if i > 0 {
						fact.Links = map[int][]int{1: {2}, 2: {1}}
					}
				}
				exp.Sources[key] = fact
				for p := 1; p <= fact.Pages; p++ {
					exp.Pages = append(exp.Pages, oracle.ExpectedPage{Text: source.FirstLine(p), Source: key, Occurrence: 1, SourcePage: p})
				}
			}
			if findings := doc.Verify(exp); len(findings) > 0 {
				t.Fatalf("oracle findings: %+v", findings)
			}
			if tc.name == "forms" && (len(a.FieldsRenamed[1]) == 0 || !codePresent(ds, diag.FormFieldsRenamed)) {
				t.Fatalf("missing actual form mappings %+v %+v", a, ds)
			}
			if strings.Contains(tc.name, "repeated") && !codePresent(ds, diag.DestinationsRenamed) {
				t.Fatal("missing rename warning")
			}
		})
	}
}

func renameNames(d *pdffixture.Doc) *pdffixture.Doc {
	for i, b := range d.Objs {
		d.Objs[i] = bytes.ReplaceAll(bytes.ReplaceAll(b, []byte("chap1"), []byte("chapA")), []byte("chap2"), []byte("chapB"))
	}
	return d
}

func TestSerializedCatalogAndVersions(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, docs := range [][]*pdffixture.Doc{
		{pdffixture.Attachment("sole")},
		{pdffixture.Plain("first"), pdffixture.Attachment("later")},
		{pdffixture.Attachment("first"), pdffixture.NamedDests("later")},
		{pdffixture.Tagged("tag"), pdffixture.Plain("later")},
		{pdffixture.Versioned("v2", "2.0", ""), pdffixture.Plain("later")},
		{pdffixture.Plain("first"), pdffixture.Versioned("v2", "2.0", "")},
	} {
		path, _, ds := assembleDocs(t, docs, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(`"/EmbeddedFiles"`)) || bytes.Contains(raw, []byte(`"/StructTreeRoot"`)) {
			t.Fatal("forbidden catalog structure survived serialization")
		}
		if slices.ContainsFunc(docs, func(d *pdffixture.Doc) bool { return d.Version == "2.0" }) && !bytes.Contains(raw, []byte(`"pdfversion": "2.0"`)) {
			t.Fatal("effective output version is not 2.0")
		}
	}
}

func TestGeneratedItemSharesContentAndFontUnion(t *testing.T) {
	font := defaultFont(t)
	page := textPage(t, font, "Ā ļ ķ Ελληνικά\nКириллица ā ļ")
	fonts, err := typeset.PrepareFonts([]*typeset.Placed{page.Blocks[0].Text})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err = pdf.WriteRepeated(t.Context(), &b, page, 130, fonts); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "generated.pdf")
	if err = os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	tools := oracle.RequireTools(t)
	if err = tools.Check(path); err != nil {
		t.Fatal(err)
	}
	count := 0
	var content, resource any
	for _, dict := range qpdfDictionaries(t, path) {
		if dict["/Type"] != "/Page" {
			continue
		}
		count++
		if content == nil {
			content = dict["/Contents"]
			resource = dict["/Resources"]
		}
		if dict["/Contents"] != content || dict["/Resources"] != resource {
			t.Fatal("generated pages did not share content/resources")
		}
	}
	if count != 130 || content == nil || resource == nil || embeddedPrograms(t, path) != 1 {
		t.Fatalf("repeated page/font structure: pages=%d", count)
	}
	raw, err := exec.CommandContext(t.Context(), tools.PDFToText, "-f", "1", "-l", "1", "-raw", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Ā ļ ķ Ελληνικά\nКириллица ā ļ") {
		t.Fatalf("extraction %q", raw)
	}
}

func TestChangedSourceAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.pdf")
	doc := pdffixture.Plain("A")
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	facts, ds := pdf.Inspect(t.Context(), doc.Bytes())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	layout, _ := packet.Resolve(packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Path: path, Ref: path}}), map[string]packet.SourceFacts{path: facts.SourceFacts}, 0)
	if err := pdffixture.Plain("B").WriteFile(path); err != nil {
		t.Fatal(err)
	}
	_, err := pdf.Assemble(t.Context(), layout, map[string]pdf.SourceFacts{path: facts}, wholeOccurrences(layout, map[string]pdf.SourceFacts{path: facts}), nil, nil, nil, nil, &bytes.Buffer{})
	if err == nil || pdf.Diagnostic(err).Code != diag.SourceChanged {
		t.Fatalf("change detection %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = pdf.Assemble(ctx, layout, nil, nil, nil, nil, nil, nil, &bytes.Buffer{})
	if err == nil || pdf.Diagnostic(err).Code != diag.Interrupted {
		t.Fatalf("cancel %v", err)
	}
}

func defaultForm(tag, resource, font, da string, q int) *pdffixture.Doc {
	d := pdffixture.Plain(tag)
	d.Objs[4] = []byte(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s >>", font))
	d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Tx /T (%s) /V (%s) /Rect [50 50 250 90] /P 3 0 R /F 4 >>", tag, tag)))
	insertEntry(d, 3, "/Annots [6 0 R]")
	insertEntry(d, 1, fmt.Sprintf("/AcroForm << /Fields [6 0 R] /DA (%s) /Q %d /DR << /Font << /%s 5 0 R >> >> /NeedAppearances true >>", da, q, resource))
	return d
}

func TestNarrowFormDefaultsDigest(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second *pdffixture.Doc
		refused       bool
	}{
		{"identical resources", defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), defaultForm("TWO", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), false},
		{"distinct resource names", defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), defaultForm("TWO", "Cour", "Courier", "/Cour 12 Tf 0 g", 0), true},
		{"same resource name different font", defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), defaultForm("TWO", "Helv", "Courier", "/Helv 12 Tf 0 g", 0), true},
		{"different inherited DA", defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), defaultForm("TWO", "Helv", "Helvetica", "/Helv 10 Tf 0 g", 0), true},
		{"different inherited Q", defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), defaultForm("TWO", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 2), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, a, ds := assembleDocs(t, []*pdffixture.Doc{tc.first, tc.second}, nil)
			if tc.refused {
				if !codePresent(ds, diag.SourceFormUnsupported) {
					t.Fatalf("unsupported form combination: %+v", ds)
				}
				return
			}
			if status, _ := diag.Outcome(ds); status != "ok" || !codePresent(ds, diag.FormFieldsRenamed) || len(a.FieldsRenamed[1]) != 1 || a.FieldsRenamed[1][0].From != "TWO" || !strings.HasSuffix(a.FieldsRenamed[1][0].To, ".TWO") {
				t.Fatalf("actual distinct-name mapping %+v %+v", a, ds)
			}
		})
	}
	// First form may appear after plain or empty-form sources.
	for _, first := range []*pdffixture.Doc{pdffixture.Plain("plain"), insertEntry(pdffixture.Plain("empty"), 1, "/AcroForm << /Fields [] >>")} {
		_, _, ds := assembleDocs(t, []*pdffixture.Doc{first, defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)}, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
	}
}

func TestMalformedWidgetRectangleIsRefused(t *testing.T) {
	d := defaultForm("bad", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
	d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("[50 50 250 90]"), []byte("[50 50 250]"))
	_, ds := pdf.Inspect(t.Context(), d.Bytes())
	if !codePresent(ds, diag.SourceInvalid) {
		t.Fatalf("malformed widget was not refused: %+v", ds)
	}
}

func TestGlyphPruningPreservesPixels(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, font := range []*typeset.Font{defaultFont(t), noSubsetFont(t, false), noSubsetFont(t, true)} {
		for _, text := range []string{"Latvija Ā ļ ķ", "Ελληνικά", "Кириллица ā ļ Áé"} {
			page := textPage(t, font, text)
			prepared, err := typeset.PrepareFonts([]*typeset.Placed{page.Blocks[0].Text})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			original := filepath.Join(dir, "original.pdf")
			pruned := filepath.Join(dir, "pruned.pdf")
			var a, b bytes.Buffer
			if _, err = pdf.Write(t.Context(), &a, []compose.Page{page}); err != nil {
				t.Fatal(err)
			}
			if _, err = pdf.WriteRepeated(t.Context(), &b, page, 1, prepared); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(original, a.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(pruned, b.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			// Read the actual serialized programs independently; matching pixels alone
			// cannot establish that the intended pruned/full program was embedded.
			_, programs := finalFontFacts(t, pruned)
			if len(programs) != 1 {
				t.Fatal("one generated font program expected")
			}
			for _, program := range programs {
				if prepared[font.Identity()].Subset {
					if len(program) >= len(font.Bytes()) {
						t.Fatal("short-text native embedding was not actually reduced")
					}
				} else if !bytes.Equal(program, font.Bytes()) {
					t.Fatal("full-font embedding changed original bytes")
				}
			}
			if got := strings.TrimSpace(extractRaw(t, pruned)[0]); got != text {
				t.Fatalf("pruned Unicode extraction %q want %q", got, text)
			}
			var pixels [][]byte
			for i, path := range []string{original, pruned} {
				prefix := filepath.Join(dir, fmt.Sprintf("render-%d", i))
				if out, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-singlefile", "-r", "72", path, prefix).CombinedOutput(); err != nil {
					t.Fatalf("render: %v %s", err, out)
				}
				data, err := os.ReadFile(prefix + ".ppm")
				if err != nil {
					t.Fatal(err)
				}
				pixels = append(pixels, data)
			}
			if !bytes.Equal(pixels[0], pixels[1]) {
				t.Fatalf("pruning changed pixels for %q", text)
			}
		}
	}
}

func TestFontObjectsDeduplicateAcrossDistinctItems(t *testing.T) {
	tools := oracle.RequireTools(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	doc := pdffixture.Plain("source")
	if err := doc.WriteFile(source); err != nil {
		t.Fatal(err)
	}
	facts, ds := pdf.Inspect(t.Context(), doc.Bytes())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	font := defaultFont(t)
	j := packet.NewPlan("", nil, nil, packet.PageStyle{}, []packet.Item{&packet.SourceItem{Path: source, Ref: "source"}})
	pages := map[int][]compose.Page{}
	var placements []*typeset.Placed
	for i, text := range []string{"Ā ļ ķ", "Ελληνικά", "Кириллица ā"} {
		page := textPage(t, font, text)
		pages[i+1] = []compose.Page{page}
		placements = append(placements, page.Blocks[0].Text)
		style := packet.Defaults()
		style.Size.Value = packet.Size{Width: 300, Height: 200}
		j.Items = append(j.Items, &packet.PageItem{Style: style, Count: 1, Ref: fmt.Sprintf("page%d", i)})
	}
	fonts, err := typeset.PrepareFonts(placements)
	if err != nil {
		t.Fatal(err)
	}
	layout, ds := packet.Resolve(packet.NewPlan(j.Output, j.FitTo, j.PageNumbers, j.PageDefaults, j.Items), map[string]packet.SourceFacts{"source": facts.SourceFacts}, 0)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	var b bytes.Buffer
	if _, err = pdf.Assemble(t.Context(), layout, map[string]pdf.SourceFacts{source: facts}, wholeOccurrences(layout, map[string]pdf.SourceFacts{source: facts}), pages, nil, fonts, nil, &b); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "output.pdf")
	if err = os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err = pdf.Verify(t.Context(), path, 4); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(raw, []byte(`"/FontFile2"`)) != 1 {
		t.Fatal("font objects were not identical/deduplicated across items")
	}
	if b.Len()-len(doc.Bytes()) >= 30000 {
		t.Fatalf("short generated text grew output by %d bytes", b.Len()-len(doc.Bytes()))
	}
	fontList, err := exec.CommandContext(t.Context(), tools.PDFFonts, path).Output()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(fontList), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 8 && strings.HasSuffix(fields[0], "+NotoSans-Regular") {
			found = true
			if fields[len(fields)-5] != "yes" || fields[len(fields)-4] != "yes" || fields[len(fields)-3] != "yes" {
				t.Fatalf("generated font embedding/subset/Unicode flags: %s", line)
			}
		}
	}
	if !found {
		t.Fatalf("generated subset font missing: %s", fontList)
	}
}

func TestFitPhysicalGeometryAndPixels(t *testing.T) {
	tools := oracle.RequireTools(t)
	source := pdffixture.WithBoxes("physical", pdffixture.PageBoxes{Media: "[0 0 306 396]", Crop: "[10 20 286 376]", Rotate: "90"}, pdffixture.PageBoxes{UserUnit: "2"}, pdffixture.PageBoxes{UserUnit: "3", Rotate: "0"})
	reference := pdffixture.WithBoxes("physical", pdffixture.PageBoxes{Media: "[0 0 306 396]", Crop: "[10 20 286 376]", Rotate: "90"}, pdffixture.PageBoxes{}, pdffixture.PageBoxes{Rotate: "0"})
	for i, scale := range []int{2, 3} {
		pageObject := 4 + 2*i
		insertEntry(reference, pageObject, fmt.Sprintf("/MediaBox [0 0 %d %d] /CropBox [%d %d %d %d]", 306*scale, 396*scale, 10*scale, 20*scale, 286*scale, 376*scale))
		streamObject := pageObject + 1
		raw := reference.Objs[streamObject-1]
		start := bytes.Index(raw, []byte("stream\n")) + len("stream\n")
		end := bytes.LastIndex(raw, []byte("\nendstream"))
		data := append([]byte(fmt.Sprintf("q %d 0 0 %d 0 0 cm\n", scale, scale)), raw[start:end]...)
		data = append(data, []byte("\nQ")...)
		reference.Objs[streamObject-1] = []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data))
	}
	for i, scale := range []int{2, 3} {
		for _, spec := range []struct {
			doc    *pdffixture.Doc
			factor int
		}{{source, 1}, {reference, scale}} {
			id := len(spec.doc.Objs) + 1
			pageID := 4 + 2*i
			factor := spec.factor
			spec.doc.Objs = append(spec.doc.Objs, []byte(fmt.Sprintf("<< /Type /Annot /Subtype /Link /P %d 0 R /Rect [%d %d %d %d] /Border [0 0 0] /A << /S /URI /URI (https://example.invalid/) >> >>", pageID, 20*factor, 40*factor, 50*factor, 80*factor)))
			insertEntry(spec.doc, pageID, fmt.Sprintf("/Annots [%d 0 R]", id))
		}
	}
	target := packet.Size{Width: 300, Height: 500}
	a, _, ds := assembleDocs(t, []*pdffixture.Doc{source}, &target)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	b, _, ds := assembleDocs(t, []*pdffixture.Doc{reference}, &target)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	for _, path := range []string{a, b} {
		doc, err := oracle.Load(tools, path)
		if err != nil {
			t.Fatal(err)
		}
		exp := oracle.Expectation{}
		for p := 1; p <= 2; p++ {
			origin := 1.0
			if path == b {
				origin = float64(p + 1)
			}
			box := []float64{10 * origin, 20 * origin, 10*origin + 300, 20*origin + 500}
			exp.Pages = append(exp.Pages, oracle.ExpectedPage{Text: doc.FirstLine(p), Geometry: &oracle.Geometry{MediaBox: box, CropBox: box, UserUnit: 1}})
		}
		if findings := doc.Verify(exp); len(findings) > 0 {
			t.Fatalf("physical target geometry: %+v", findings)
		}
	}
	sourceOutput, err := oracle.Load(tools, a)
	if err != nil {
		t.Fatal(err)
	}
	referenceOutput, err := oracle.Load(tools, b)
	if err != nil {
		t.Fatal(err)
	}
	for page := 1; page <= 2; page++ {
		actual, err := sourceOutput.AnnotationRects(page)
		if err != nil {
			t.Fatal(err)
		}
		control, err := referenceOutput.AnnotationRects(page)
		if err != nil {
			t.Fatal(err)
		}
		if len(actual) != 1 || len(control) != 1 {
			t.Fatal("fit omitted link annotation")
		}
		for coordinate := range 4 {
			origin := 10.0
			if coordinate%2 == 1 {
				origin = 20
			}
			want := control[0][coordinate] - origin*float64(page+1)
			got := actual[0][coordinate] - origin
			if math.Abs(want-got) > 1e-8 {
				t.Fatalf("physical annotation geometry differs: page %d got %v control %v", page, actual, control)
			}
		}
	}
	dir := t.TempDir()
	for page := 1; page <= 2; page++ {
		var pixels [][]byte
		for i, path := range []string{a, b} {
			prefix := filepath.Join(dir, fmt.Sprintf("render-%d-%d", page, i))
			out, err := exec.CommandContext(t.Context(), tools.PDFToPPM, "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-singlefile", "-cropbox", "-r", "30", path, prefix).CombinedOutput()
			if err != nil {
				t.Fatalf("fit rendering %v %s", err, out)
			}
			data, err := os.ReadFile(prefix + ".ppm")
			if err != nil {
				t.Fatal(err)
			}
			pixels = append(pixels, data)
		}
		if !bytes.Equal(pixels[0], pixels[1]) {
			t.Fatalf("UserUnit normalization changed physical pixels on page %d", page)
		}
	}
	_, _, ds = assembleDocs(t, []*pdffixture.Doc{pdffixture.Form("widget")}, &target)
	if !codePresent(ds, diag.FitFormUnsupported) {
		t.Fatalf("fitted widget not refused: %+v", ds)
	}
}

func namedOutline(d *pdffixture.Doc, mode string) *pdffixture.Doc {
	root, first, second := len(d.Objs)+1, len(d.Objs)+2, len(d.Objs)+3
	insertEntry(d, 1, fmt.Sprintf("/Outlines %d 0 R", root))
	one, two := "/Dest (chap1)", "/Dest /chap2"
	if mode == "actions" {
		one = "/A << /S /GoTo /D (chap1) /Next [<< /S /GoTo /D /chap2 >>] >>"
		two = "/A << /S /GoTo /D /chap2 >>"
	}
	d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count 2 >>", first, second)), []byte(fmt.Sprintf("<< /Title (First) /Parent %d 0 R /Next %d 0 R %s >>", root, second, one)), []byte(fmt.Sprintf("<< /Title (Second) /Parent %d 0 R /Prev %d 0 R %s >>", root, first, two)))
	return d
}

func TestNamedOutlinesAndActionChains(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, legacy := range []bool{false, true} {
		for _, mode := range []string{"dest", "actions"} {
			factory := pdffixture.NamedDests
			if legacy {
				factory = pdffixture.LegacyDests
			}
			path, _, ds := assembleDocs(t, []*pdffixture.Doc{pdffixture.Plain("first"), namedOutline(factory("N"), mode), namedOutline(factory("N"), mode)}, nil)
			if status, _ := diag.Outcome(ds); status != "ok" {
				t.Fatalf("legacy=%v mode=%s: %+v", legacy, mode, ds)
			}
			doc, err := oracle.Load(tools, path)
			if err != nil {
				t.Fatal(err)
			}
			targets, err := doc.OutlineTargets()
			expected := []int{2, 3, 4, 5}
			if mode == "actions" {
				expected = []int{2, 3, 3, 4, 5, 5}
			}
			if err != nil || !slices.Equal(targets, expected) {
				t.Fatalf("legacy=%v mode=%s outlines %v want %v: %v", legacy, mode, targets, expected, err)
			}
			for page, want := range map[int][]int{2: {3}, 3: {2}, 4: {5}, 5: {4}} {
				targets, err := doc.ActionTargets(page)
				if err != nil || !slices.Equal(targets, want) {
					t.Fatalf("page %d actions %v want %v: %v", page, targets, want, err)
				}
			}
		}
	}
}

func TestManyDestinationCollisionsAndBalancedNameTree(t *testing.T) {
	tools := oracle.RequireTools(t)
	docs := make([]*pdffixture.Doc, 70)
	for i := range docs {
		docs[i] = pdffixture.NamedDests("N")
	}
	path, _, ds := assembleDocs(t, docs, nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	doc, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	for occurrence := range 70 {
		for offset, want := range []int{occurrence*2 + 2, occurrence*2 + 1} {
			page := occurrence*2 + offset + 1
			targets, err := doc.ActionTargets(page)
			if err != nil || !slices.Equal(targets, []int{want}) {
				t.Fatalf("page %d -> %v, want %d: %v", page, targets, want, err)
			}
		}
	}
}

func TestDestinationOutsideSourceIsOmitted(t *testing.T) {
	d := pdffixture.NamedDests("outside")
	// Keep a valid Page object that is not a member of the source's page tree.
	orphan := len(d.Objs) + 1
	d.Objs = append(d.Objs, bytes.Clone(d.Objs[2]))
	d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("(chap2) [4 0 R"), []byte(fmt.Sprintf("(chap2) [%d 0 R", orphan)))
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{d}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" || !codePresent(ds, diag.DestinationsDropped) {
		t.Fatalf("outside destination: %+v", ds)
	}
	tools := oracle.RequireTools(t)
	doc, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := doc.ActionTargets(1)
	if err != nil || len(targets) != 0 {
		t.Fatalf("omitted target left a dangling link: %v %v", targets, err)
	}
}

func TestHybridXFAAndDocumentScriptCachesAreRemoved(t *testing.T) {
	hybrid := defaultForm("hybrid", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
	xml := []byte("<template/>")
	hybrid.Objs = append(hybrid.Objs, []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(xml), xml)))
	hybrid.Objs[0] = bytes.ReplaceAll(hybrid.Objs[0], []byte("/NeedAppearances true"), []byte("/NeedAppearances true /XFA [(template) 7 0 R]"))
	script := pdffixture.Plain("script")
	script.Objs = append(script.Objs, []byte("<< /S /JavaScript /JS (app.alert\\(1\\)) >>"))
	insertEntry(script, 1, "/Names << /JavaScript << /Names [(launch) 6 0 R] >> >>")
	tools := oracle.RequireTools(t)
	for _, docs := range [][]*pdffixture.Doc{{hybrid}, {script, pdffixture.NamedDests("N")}, {pdffixture.NamedDests("N"), script}} {
		path, _, ds := assembleDocs(t, docs, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(`"/XFA"`)) || bytes.Contains(raw, []byte(`"/JavaScript"`)) {
			t.Fatal("forbidden cached structures survived writing")
		}
	}
}

func TestPageActionsAndRelativeURIBytesSurvive(t *testing.T) {
	tools := oracle.RequireTools(t)
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{pdffixture.PageActions("actions"), pdffixture.PageActions("actions")}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	doc, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range map[int]int{1: 2, 2: 1, 3: 4, 4: 3} {
		targets, err := doc.ActionTargets(page)
		if err != nil || !slices.Equal(targets, []int{want}) {
			t.Fatalf("page %d -> %v want %d: %v", page, targets, want, err)
		}
	}
	path, _, ds = assembleDocs(t, []*pdffixture.Doc{pdffixture.URILink("relative", "relative/path", "https://example.invalid/", false), pdffixture.URILink("relative", "relative/path", "https://example.invalid/", false)}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" || !codePresent(ds, diag.SourceURIBaseRemoved) {
		t.Fatal(ds)
	}
	raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("u:relative/path")) || bytes.Contains(raw, []byte(`"/Base"`)) {
		t.Fatal("URI bytes/base policy mismatch")
	}
}

func FuzzInspect(f *testing.F) {
	f.Add(pdffixture.Plain("seed").Bytes())
	f.Add(pdffixture.NamedDests("seed").Bytes())
	f.Add([]byte("%PDF-1.7\n"))
	if data, err := os.ReadFile("testdata/malformed-form-fields-panic.pdf"); err == nil {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip("small parser seeds only")
		}
		_, _ = pdf.Inspect(t.Context(), data)
	})
}

func TestResourceDictionarySharedWithAnnotationIsRefused(t *testing.T) {
	d := pdffixture.Plain("shared")
	d.Objs = append(d.Objs, []byte("<< /Type /Annot /Subtype /Link /Font << /F1 5 0 R >> /Rect [10 10 90 90] /Border [0 0 0] /A << /S /URI /URI (https://example.invalid/) >> >>"))
	d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Resources << /Font << /F1 5 0 R >> >>"), []byte("/Resources 6 0 R"))
	insertEntry(d, 3, "/Annots [6 0 R]")
	_, ds := pdf.Inspect(t.Context(), d.Bytes())
	if !codePresent(ds, diag.SourceInvalid) {
		t.Fatalf("shared dictionary role was not refused: %+v", ds)
	}
}

func TestHexFormValuesAndCommentSeparatedReferences(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, tc := range []struct{ raw, want string }{{"<48 65 6C 6C 6F 20 57 6F 72 6C 64>", "u:Hello World"}, {"<414>", "u:A@"}, {"<" + strings.Repeat("41 ", 2048) + ">", "u:" + strings.Repeat("A", 2048)}} {
		d := defaultForm("name", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
		d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/V (name)"), []byte("/V "+tc.raw))
		path, _, ds := assembleDocs(t, []*pdffixture.Doc{d, d}, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		raw, err := exec.CommandContext(t.Context(), tools.QPDF, "--json", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(tc.want)) {
			t.Fatalf("hex value %s lost %s", tc.raw, tc.want)
		}
	}
	for _, ref := range []string{"4%first\n0 R", "4 0%second\nR"} {
		d := pdffixture.Plain("comment-reference")
		d.Objs[2] = bytes.ReplaceAll(d.Objs[2], []byte("/Contents 4 0 R"), []byte("/Contents "+ref))
		f, ds := pdf.Inspect(t.Context(), d.Bytes())
		if status, _ := diag.Outcome(ds); status != "ok" {
			if !codePresent(ds, diag.SourceInvalid) {
				t.Fatalf("valid comment reference produced unexpected refusal: %+v", ds)
			}
			t.Logf("Upstream rejects legal comment-separated reference %q: %+v", ref, ds)
			continue
		}
		if f.Pages != 1 {
			t.Fatal("comment reference changed page count")
		}
		path, _, ds := assembleDocs(t, []*pdffixture.Doc{d}, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		doc, err := oracle.Load(tools, path)
		if err != nil {
			t.Fatal(err)
		}
		if doc.FirstLine(1) != "comment-reference" {
			t.Fatalf("comment-separated reference lost visible content: %q", doc.FirstLine(1))
		}
	}
}

func TestEmptyFormDefaultsCannotDisplaceLaterFieldResources(t *testing.T) {
	tools := oracle.RequireTools(t)
	empty := pdffixture.Plain("empty")
	empty.Objs[4] = []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>")
	insertEntry(empty, 1, "/AcroForm << /Fields [] /DA (/Cour 12 Tf 0 g) /DR << /Font << /Cour 5 0 R >> >> >>")
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{empty, defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	document, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	if fonts := document.FormDefaultFontNames(); !slices.Equal(fonts, []string{"Helv"}) {
		t.Fatalf("later form default resources displaced: %v", fonts)
	}
	command := exec.CommandContext(t.Context(), tools.PDFToPPM, "-f", "2", "-l", "2", "-singlefile", "-r", "10", path, filepath.Join(t.TempDir(), "form"))
	output, err := command.CombinedOutput()
	if err != nil || len(output) > 0 {
		t.Fatalf("later form resources were displaced by empty form defaults: %v %s", err, output)
	}
}

func TestDistinctNamedOutlinesAndUnicodeDestinations(t *testing.T) {
	tools := oracle.RequireTools(t)
	for _, docs := range [][]*pdffixture.Doc{
		{namedOutline(pdffixture.NamedDests("A"), "actions"), renameNames(namedOutline(pdffixture.NamedDests("B"), "actions"))},
		{namedOutline(pdffixture.LegacyDests("A"), "actions"), renameNames(namedOutline(pdffixture.LegacyDests("B"), "actions"))},
	} {
		path, _, ds := assembleDocs(t, docs, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		doc, err := oracle.Load(tools, path)
		if err != nil {
			t.Fatal(err)
		}
		targets, err := doc.OutlineTargets()
		if err != nil || !slices.Equal(targets, []int{1, 2, 2, 3, 4, 4}) {
			t.Fatalf("distinct named outlines %v %v", targets, err)
		}
	}
	unicode := pdffixture.NamedDests("unicode")
	for i, body := range unicode.Objs {
		unicode.Objs[i] = bytes.ReplaceAll(bytes.ReplaceAll(body, []byte("(chap1)"), []byte("<FEFFD83DDE00>")), []byte("(chap2)"), []byte("<FEFFE000>"))
	}
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{unicode, unicode}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	doc, err := oracle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range map[int]int{1: 2, 2: 1, 3: 4, 4: 3} {
		targets, err := doc.ActionTargets(page)
		if err != nil || !slices.Equal(targets, []int{want}) {
			t.Fatalf("Unicode name target %d -> %v %v", page, targets, err)
		}
	}
}

func TestUnsignedSignatureNullValuesAndIndirectRenderingFlag(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		d := pdffixture.EmptySignatureField("unsigned-null")
		value := "null"
		if indirect {
			d.Objs = append(d.Objs, []byte("null"))
			value = fmt.Sprintf("%d 0 R", len(d.Objs))
		}
		insertEntry(d, 6, "/V "+value)
		_, ds := pdf.Inspect(t.Context(), d.Bytes())
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Errorf("unsigned null signature indirect=%v: %+v", indirect, ds)
		}
	}
	d := pdffixture.Plain("indirect-needs-rendering")
	d.Objs = append(d.Objs, []byte("true"))
	insertEntry(d, 1, "/NeedsRendering 6 0 R")
	_, ds := pdf.Inspect(t.Context(), d.Bytes())
	if !codePresent(ds, diag.SourceDynamicForm) {
		t.Errorf("indirect NeedsRendering true was not refused: %+v", ds)
	}
}

func TestIndirectPolicyAndActionValues(t *testing.T) {
	t.Run("signature type", func(t *testing.T) {
		d := pdffixture.SignatureValue("indirect-signature")
		d.Objs[5] = bytes.ReplaceAll(bytes.ReplaceAll(d.Objs[5], []byte("/FT /Sig"), []byte("/FT 7 0 R")), []byte("/Type /Sig"), nil)
		d.Objs = append(d.Objs, []byte("/Sig"))
		_, ds := pdf.Inspect(t.Context(), d.Bytes())
		if !codePresent(ds, diag.SourceSigned) {
			t.Fatalf("indirect field type: %+v", ds)
		}
	})
	t.Run("widget subtype", func(t *testing.T) {
		d := defaultForm("widget", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
		d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("/Subtype /Widget"), []byte("/Subtype 7 0 R"))
		d.Objs = append(d.Objs, []byte("/Widget"))
		f, ds := pdf.Inspect(t.Context(), d.Bytes())
		if status, _ := diag.Outcome(ds); status != "ok" || !f.HasWidgets {
			t.Fatalf("indirect widget: %+v %+v", f, ds)
		}
	})
	t.Run("appearance flag", func(t *testing.T) {
		d := defaultForm("appearance", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0)
		d.Objs[0] = bytes.ReplaceAll(d.Objs[0], []byte("/NeedAppearances true"), []byte("/NeedAppearances 7 0 R"))
		d.Objs = append(d.Objs, []byte("true"))
		_, ds := pdf.Inspect(t.Context(), d.Bytes())
		if !codePresent(ds, diag.FormAppearanceRegenerationRequested) {
			t.Fatalf("indirect appearance flag: %+v", ds)
		}
	})
	t.Run("URI action and text", func(t *testing.T) {
		d := pdffixture.URILink("indirect-uri", "relative/path", "https://example.invalid/", false)
		d.Objs[5] = bytes.ReplaceAll(bytes.ReplaceAll(d.Objs[5], []byte("/S /URI"), []byte("/S 7 0 R")), []byte("/URI (relative/path)"), []byte("/URI 8 0 R"))
		d.Objs = append(d.Objs, []byte("/URI"), []byte("(relative/path)"))
		_, ds := pdf.Inspect(t.Context(), d.Bytes())
		if !codePresent(ds, diag.SourceURIBaseRemoved) {
			t.Fatalf("indirect URI: %+v", ds)
		}
	})
	t.Run("GoTo action type", func(t *testing.T) {
		d := pdffixture.NamedDests("indirect-GoTo")
		object := len(d.Objs) + 1
		for i, body := range d.Objs {
			d.Objs[i] = bytes.ReplaceAll(body, []byte("/S /GoTo"), []byte(fmt.Sprintf("/S %d 0 R", object)))
		}
		d.Objs = append(d.Objs, []byte("/GoTo"))
		path, _, ds := assembleDocs(t, []*pdffixture.Doc{d, d}, nil)
		if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatal(ds)
		}
		doc, err := oracle.Load(oracle.RequireTools(t), path)
		if err != nil {
			t.Fatal(err)
		}
		targets, err := doc.ActionTargets(4)
		if err != nil || !slices.Equal(targets, []int{3}) {
			t.Fatalf("indirect GoTo crossed occurrences: %v %v", targets, err)
		}
	})
}

func TestSignatureFieldTypeInheritsThroughIndirectNull(t *testing.T) {
	for _, signed := range []bool{false, true} {
		d := pdffixture.EmptySignatureField("null-inheritance")
		if signed {
			d = pdffixture.SignatureValue("null-inheritance")
		}
		d.Objs[0] = bytes.ReplaceAll(d.Objs[0], []byte("/Fields [6 0 R]"), []byte("/Fields [7 0 R]"))
		d.Objs[5] = bytes.ReplaceAll(bytes.ReplaceAll(d.Objs[5], []byte("/FT /Sig"), []byte("/FT 8 0 R")), []byte("/Type /Sig"), nil)
		insertEntry(d, 6, "/Parent 7 0 R")
		d.Objs = append(d.Objs, []byte("<< /FT /Sig /T (group) /Kids [6 0 R] >>"), []byte("null"))
		_, ds := pdf.Inspect(t.Context(), d.Bytes())
		if signed {
			if !codePresent(ds, diag.SourceSigned) {
				t.Fatalf("inherited signature type missed: %+v", ds)
			}
		} else if status, _ := diag.Outcome(ds); status != "ok" {
			t.Fatalf("unsigned inherited signature refused: %+v", ds)
		}
	}
}

func TestUnnamedNonemptyFormsUseTheSameResourceBoundary(t *testing.T) {
	_, _, ds := assembleDocs(t, []*pdffixture.Doc{defaultForm("ONE", "Helv", "Helvetica", "/Helv 12 Tf 0 g", 0), defaultForm("", "Cour", "Courier", "/Cour 12 Tf 0 g", 0)}, nil)
	if !codePresent(ds, diag.SourceFormUnsupported) {
		t.Fatalf("unnamed fields bypassed resource compatibility: %+v", ds)
	}
}

func TestDroppedDestinationInNextArrayLeavesValidActions(t *testing.T) {
	d := pdffixture.NamedDests("dropped-next")
	orphan := len(d.Objs) + 1
	d.Objs = append(d.Objs, bytes.Clone(d.Objs[2]))
	d.Objs[5] = bytes.ReplaceAll(d.Objs[5], []byte("(chap2) [4 0 R"), []byte(fmt.Sprintf("(chap2) [%d 0 R", orphan)))
	// Page 1 now has one valid action followed by one unusable and one valid action.
	d.Objs[9] = bytes.ReplaceAll(d.Objs[9], []byte("/Dest (chap2)"), []byte("/A << /S /GoTo /D (chap1) /Next [<< /S /GoTo /D (chap2) >> << /S /GoTo /D (chap1) >>] >>"))
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{d}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" || !codePresent(ds, diag.DestinationsDropped) {
		t.Fatal(ds)
	}
	doc, err := oracle.Load(oracle.RequireTools(t), path)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := doc.ActionTargets(1)
	if err != nil || !slices.Equal(targets, []int{1, 1}) {
		t.Fatalf("valid Next actions were lost or left dangling: %v %v", targets, err)
	}
}

func TestConflictingDestinationNamespacesAreRefused(t *testing.T) {
	for _, same := range []bool{false, true} {
		d := pdffixture.NamedDests("both")
		target := 4
		if same {
			target = 3
		}
		insertEntry(d, 1, fmt.Sprintf("/Dests << /chap1 [%d 0 R /Fit] >>", target))
		_, ds := pdf.Inspect(t.Context(), d.Bytes())
		if same {
			if status, _ := diag.Outcome(ds); status != "ok" {
				t.Fatalf("equivalent namespaces refused: %+v", ds)
			}
		} else if !codePresent(ds, diag.SourceInvalid) {
			t.Fatalf("conflicting namespaces silently conflated: %+v", ds)
		}
	}
}

func TestDestinationByteEncodingsStayDistinct(t *testing.T) {
	d := pdffixture.NamedDests("bytes")
	// ASCII chap1 and UTF-16BE chap1 are distinct byte-string keys and targets.
	d.Objs[5] = []byte("<< /Names [(chap1) [3 0 R /Fit] <FEFF00630068006100700031> [4 0 R /Fit]] >>")
	d.Objs[9] = bytes.ReplaceAll(d.Objs[9], []byte("(chap2)"), []byte("<FEFF00630068006100700031>"))
	path, _, ds := assembleDocs(t, []*pdffixture.Doc{d}, nil)
	if status, _ := diag.Outcome(ds); status != "ok" {
		t.Fatal(ds)
	}
	if err := oracle.RequireTools(t).CheckLinks(path); err != nil {
		t.Fatal(err)
	}
}
