// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdforacle_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/pdforacle"
)

// defect corrupts one thing in a known-good document, or in the expectation, and names the check that
// must fail for it. (qpdf's own structure check may also object.)
type defect struct {
	doc    func() *pdffixture.Doc
	expect func(exp *pdforacle.Expectation)
	name   string
	want   string
}

const (
	// docSource names the source of twoPageExpectation.
	docSource = "doc"

	// orphanPage is a page object that no page tree reaches.
	orphanPage = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"
)

// twoPageExpectation describes a two-page document "<tag> p1", "<tag> p2" whose links lead to the other page.
func twoPageExpectation(tag string) pdforacle.Expectation {
	letter := pdforacle.Geometry{MediaBox: []float64{0, 0, 612, 792}}

	return pdforacle.Expectation{
		Pages: []pdforacle.ExpectedPage{
			{Text: tag + " p1", Source: docSource, Occurrence: 1, SourcePage: 1},
			{Text: tag + " p2", Source: docSource, Occurrence: 1, SourcePage: 2},
		},
		Sources: map[string]pdforacle.SourceFact{docSource: {
			Pages: 2, Version: 17, Links: map[int][]int{1: {2}, 2: {1}},
			Geometry: map[int]pdforacle.Geometry{1: letter, 2: letter},
		}},
	}
}

func load(t *testing.T, tools pdforacle.Tools, doc *pdffixture.Doc) *pdforacle.Document {
	t.Helper()

	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := doc.WriteFile(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := pdforacle.Load(tools, path)
	if err != nil {
		t.Fatal(err)
	}

	return loaded
}

func set(doc *pdffixture.Doc, number int, body string) { doc.Objs[number-1] = []byte(body) }

// TestVerifyAcceptsCleanDocuments is the positive control for the negative controls below.
func TestVerifyAcceptsCleanDocuments(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)

	for name, doc := range map[string]*pdffixture.Doc{
		"links": pdffixture.Links("D"), "named": pdffixture.NamedDests("D"), "legacy": pdffixture.LegacyDests("D"),
		"form": pdffixture.Form("D"), "outlined": pdffixture.Outlined("D"), "actions": pdffixture.PageActions("D"),
	} {
		exp := twoPageExpectation("D")
		if name == "form" || name == "outlined" {
			fact := exp.Sources[docSource]
			fact.Links = nil
			exp.Sources[docSource] = fact
		}

		if findings := load(t, tools, doc).Verify(exp); len(findings) != 0 {
			t.Errorf("%s: unexpected findings %v", name, findings)
		}
	}
}

func linksDoc() *pdffixture.Doc { return pdffixture.Links("D") }

// withLinkAnnotation is the links fixture with its first link annotation (object 5) replaced.
func withLinkAnnotation(body string) func() *pdffixture.Doc {
	return func() *pdffixture.Doc {
		doc := linksDoc()
		set(doc, 5, body)

		return doc
	}
}

// linkDefects corrupt link annotations, their ownership and their destinations.
func linkDefects() []defect {
	return []defect{
		{
			name: "link to null destination", want: pdforacle.CheckLinkDangling,
			doc: withLinkAnnotation("<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] /P 3 0 R /Dest [null /Fit] >>"),
		},
		{
			name: "link to a page outside the tree", want: pdforacle.CheckLinkInTree,
			doc: func() *pdffixture.Doc {
				doc := withLinkAnnotation("<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] /P 3 0 R /Dest [13 0 R /Fit] >>")()
				doc.Objs = append(doc.Objs, []byte(orphanPage))

				return doc
			},
		},
		{
			name: "annotation listed on two pages", want: pdforacle.CheckAnnotMembership,
			doc: func() *pdffixture.Doc {
				doc := linksDoc()
				set(doc, 11, "[5 0 R 12 0 R]")

				return doc
			},
		},
		{
			name: "annotation /P names another page", want: pdforacle.CheckAnnotParent,
			doc: withLinkAnnotation("<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] /P 4 0 R /Dest [4 0 R /Fit] >>"),
		},
		{
			name: "named destination to a missing page", want: pdforacle.CheckNamedDests,
			doc: func() *pdffixture.Doc {
				doc := pdffixture.NamedDests("D")
				set(doc, 6, "<< /Names [(chap1) [3 0 R /Fit] (chap2) [99 0 R /Fit]] >>")

				return doc
			},
		},
	}
}

// formDefects corrupt form fields and widgets.
func formDefects() []defect {
	return []defect{
		{
			name: "form widget whose field is not listed", want: pdforacle.CheckFormRoots,
			doc: func() *pdffixture.Doc {
				doc := pdffixture.Form("D")
				set(doc, 1, "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [21 0 R] >> >>")

				return doc
			},
		},
		{
			name: "form field without a widget on a page", want: pdforacle.CheckFormWidgets,
			doc: func() *pdffixture.Doc {
				doc := pdffixture.Form("D")
				doc.Objs = append(doc.Objs, []byte("<< /FT /Tx /T (ghost) >>"))
				set(doc, 1, "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [20 0 R 21 0 R 24 0 R] >> >>")

				return doc
			},
		},
		{
			name: "form widget on two pages", want: pdforacle.CheckFormWidgetsShared,
			doc: func() *pdffixture.Doc {
				doc := pdffixture.Form("D")
				set(doc, 4, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 9 0 R >> >> "+
					"/Contents 8 0 R /Annots [23 0 R 21 0 R] >>")

				return doc
			},
		},
	}
}

// documentDefects corrupt the outline and the page objects.
func documentDefects() []defect {
	return []defect{
		{
			name: "outline to a missing destination", want: pdforacle.CheckOutlines,
			doc: func() *pdffixture.Doc {
				doc := pdffixture.Outlined("D")
				set(doc, 9, "<< /Title (First) /Parent 8 0 R /Next 10 0 R /Dest [null /Fit] >>")

				return doc
			},
		},
		{
			name: "page object outside the page tree", want: pdforacle.CheckOrphanPages,
			doc: func() *pdffixture.Doc {
				doc := linksDoc()
				doc.Objs = append(doc.Objs, []byte(orphanPage))

				return doc
			},
		},
	}
}

// expectationDefects leave the document intact and make the expectation wrong.
func expectationDefects() []defect {
	return []defect{
		{
			name: "link reaches the wrong source page", want: pdforacle.CheckLinkPage, doc: linksDoc,
			expect: func(exp *pdforacle.Expectation) {
				fact := exp.Sources[docSource]
				fact.Links = map[int][]int{1: {1}, 2: {2}}
				exp.Sources[docSource] = fact
			},
		},
		{
			name: "link reaches another occurrence", want: pdforacle.CheckLinkOccurrence, doc: linksDoc,
			expect: func(exp *pdforacle.Expectation) { exp.Pages[1].Occurrence = 2 },
		},
		{
			name: "version below the source's", want: pdforacle.CheckVersion, doc: linksDoc,
			expect: func(exp *pdforacle.Expectation) {
				fact := exp.Sources[docSource]
				fact.Version = 20
				exp.Sources[docSource] = fact
			},
		},
		{
			name: "wrong text", want: pdforacle.CheckPageText, doc: linksDoc,
			expect: func(exp *pdforacle.Expectation) { exp.Pages[0].Text = "other" },
		},
		{
			name: "page count", want: pdforacle.CheckPageCount, doc: linksDoc,
			expect: func(exp *pdforacle.Expectation) { exp.Pages = exp.Pages[:1] },
		},
		{
			name: "geometry", want: pdforacle.CheckGeometry, doc: linksDoc,
			expect: func(exp *pdforacle.Expectation) {
				fact := exp.Sources[docSource]
				fact.Geometry = map[int]pdforacle.Geometry{1: {MediaBox: []float64{0, 0, 1, 1}}}
				exp.Sources[docSource] = fact
			},
		},
		{
			name: "rotation and user unit", want: pdforacle.CheckGeometry, doc: linksDoc,
			expect: func(exp *pdforacle.Expectation) {
				fact := exp.Sources[docSource]
				fact.Geometry = map[int]pdforacle.Geometry{1: {MediaBox: []float64{0, 0, 612, 792}, Rotate: 90, UserUnit: 2}}
				exp.Sources[docSource] = fact
			},
		},
	}
}

// TestVerifyDetectsEachDefect corrupts one thing at a time in a known-good document; the check for that
// defect must fail.
func TestVerifyDetectsEachDefect(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)

	for _, tc := range slices.Concat(linkDefects(), formDefects(), documentDefects(), expectationDefects()) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			exp := twoPageExpectation("D")
			if tc.expect != nil {
				tc.expect(&exp)
			}

			findings := load(t, tools, tc.doc()).Verify(exp)

			if !slices.Contains(pdforacle.Failed(findings), tc.want) {
				t.Fatalf("check %q did not fail; findings: %v", tc.want, findings)
			}
		})
	}
}

func TestVerifyDetectsStructuralDamage(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)

	path := filepath.Join(t.TempDir(), "damaged.pdf")

	damaged := strings.Replace(string(pdffixture.Links("D").Bytes()), "0000000015 00000 n", "0000009999 00000 n", 1)
	if err := os.WriteFile(path, []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}

	doc, err := pdforacle.Load(tools, path)
	if err != nil {
		t.Skipf("qpdf cannot even dump the damaged file: %v", err)
	}

	if !slices.Contains(pdforacle.Failed(doc.Verify(twoPageExpectation("D"))), pdforacle.CheckStructure) {
		t.Fatal("damaged cross-reference table not detected")
	}
}

// boxedDocument is a two-page document with a rotated, scaled first page and a titled /Info.
func boxedDocument(t *testing.T, tools pdforacle.Tools) *pdforacle.Document {
	t.Helper()

	boxes := pdffixture.WithBoxes(
		"U",
		pdffixture.PageBoxes{},
		pdffixture.PageBoxes{Media: "[0 0 100 50]", Rotate: "90", UserUnit: "2"},
		pdffixture.PageBoxes{Media: "[0 0 20 30]"},
	)
	boxes.Info = "<< /Title (T) >>"

	return load(t, tools, boxes)
}

func TestDocumentPageAccessors(t *testing.T) {
	t.Parallel()

	doc := boxedDocument(t, pdforacle.RequireTools(t))

	if doc.PageCount() != 2 || len(doc.PageObjects()) != 2 {
		t.Fatalf("pages %d / %v", doc.PageCount(), doc.PageObjects())
	}

	if doc.ObjectCount() == 0 {
		t.Error("no objects")
	}

	if got := []float64{doc.UserUnit(1), doc.UserUnit(2), doc.UserUnit(0), doc.UserUnit(3)}; !slices.Equal(got, []float64{2, 1, 0, 0}) {
		t.Errorf("user units of pages 1, 2, 0 and 3: %v", got)
	}

	if doc.FirstLine(0) != "" || doc.FirstLine(9) != "" {
		t.Error("FirstLine accepted an out-of-range page")
	}

	if keys := doc.CatalogKeys(); !slices.Contains(keys, "/Pages") {
		t.Errorf("catalog keys %v", keys)
	}

	if doc.Attachments() != 0 || pdforacle.Failed(nil) == nil {
		t.Error("attachments or Failed misreport")
	}
}

func TestDocumentVersionAccessors(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)
	doc := boxedDocument(t, tools)

	if !slices.Contains(doc.InfoKeys(), "/Title") || doc.CatalogVersion() != "" || doc.EffectiveVersion() != 17 {
		t.Errorf("info %v, catalog version %q, version %d", doc.InfoKeys(), doc.CatalogVersion(), doc.EffectiveVersion())
	}

	catalogVersioned := load(t, tools, pdffixture.Versioned("V", "1.4", "1.6"))
	if catalogVersioned.CatalogVersion() != "1.6" || catalogVersioned.EffectiveVersion() != 16 {
		t.Errorf("catalog version %q effective %d", catalogVersioned.CatalogVersion(), catalogVersioned.EffectiveVersion())
	}
}

func TestDocumentRendering(t *testing.T) {
	t.Parallel()

	doc := boxedDocument(t, pdforacle.RequireTools(t))

	if width, height, err := doc.RenderedSize(1); err != nil || width != 50 || height != 100 {
		t.Errorf("rendered size %d x %d: %v", width, height, err)
	}

	if _, err := doc.PixelColor(1, 5000, 5000); err == nil {
		t.Error("PixelColor accepted a pixel outside the page")
	}

	if _, err := doc.PixelColor(9, 0, 0); err == nil {
		t.Error("PixelColor accepted a missing page")
	}

	if rgb, err := doc.PixelColor(1, 1, 1); err != nil || rgb != [3]uint8{255, 255, 255} {
		t.Errorf("blank page pixel %v: %v", rgb, err)
	}

	if _, _, err := doc.RenderedSize(9); err == nil {
		t.Error("RenderedSize accepted a missing page")
	}
}

func TestLoadReportsErrors(t *testing.T) {
	t.Parallel()

	tools := pdforacle.RequireTools(t)

	if _, err := pdforacle.Load(tools, filepath.Join(t.TempDir(), "absent.pdf")); err == nil {
		t.Error("Load accepted a missing file")
	}

	if _, err := pdforacle.Load(pdforacle.Tools{QPDF: tools.QPDF, PDFToText: "/nonexistent/pdftotext"}, writeValid(t)); err == nil {
		t.Error("Load accepted a missing pdftotext")
	}
}

func writeValid(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ok.pdf")
	if err := pdffixture.Plain("P").WriteFile(path); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestFindToolsReportsAMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if _, err := pdforacle.FindTools(); err != nil {
		// A Homebrew prefix may still supply the tools; either outcome is valid, a failure must name a tool.
		if !strings.Contains(err.Error(), "look up") {
			t.Errorf("error does not name the tool lookup: %v", err)
		}
	}
}

func TestRequiredToolsRejectMissingPrerequisite(t *testing.T) {
	t.Parallel()

	if os.Getenv("PDFCONCAT_QA_MISSING_HELPER") == "1" {
		pdforacle.RequireTools(t)
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	command := exectest.Command(t.Context(), executable, "-test.run=^TestRequiredToolsRejectMissingPrerequisite$")
	command.Env = append(os.Environ(), "PATH="+t.TempDir(), pdforacle.RequireToolsEnv+"=1", "PDFCONCAT_QA_MISSING_HELPER=1")

	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "QA tools are required") || strings.Contains(string(output), "SKIP") {
		t.Fatalf("missing required tool must fail the real test process: %v\n%s", err, output)
	}
}
