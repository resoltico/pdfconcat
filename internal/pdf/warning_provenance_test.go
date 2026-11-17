// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func warningEntry(d *pdffixture.Doc, object int, entry string) {
	raw := d.Objs[object-1]
	end := bytes.LastIndex(raw, []byte(">>"))
	if end < 0 {
		panic("fixture object is not a dictionary")
	}
	d.Objs[object-1] = append(append(append([]byte{}, raw[:end]...), []byte(" "+entry+" ")...), raw[end:]...)
}

func warningFindings(t *testing.T, d *pdffixture.Doc, requests ...SelectionRequest) []diag.Diagnostic {
	t.Helper()
	_, _, ds := Inspect(t.Context(), d.Bytes(), "", requests)
	if status := diag.Outcome(ds); status != "ok" {
		t.Fatalf("fixture admission: %+v", ds)
	}
	return ds
}

func warningCode(ds []diag.Diagnostic, code diag.Code) []diag.Diagnostic {
	var found []diag.Diagnostic
	for _, d := range ds {
		if d.Code == code {
			found = append(found, d)
		}
	}
	return found
}

func warningLocation(t *testing.T, d diag.Diagnostic, object, page int, path string) {
	t.Helper()
	want := diag.PDFLocation{Object: object, Page: page, Path: path}
	if d.PDFLocation == nil || *d.PDFLocation != want {
		t.Fatalf("%s location = %+v, want %+v (detail %q)", d.Code, d.PDFLocation, want, d.Detail)
	}
}

func TestWarningCatalogDispositionSourceSites(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("indirect-names-%t", indirect), func(t *testing.T) {
			doc := pdffixture.Plain("catalog")
			doc.Objs = append(doc.Objs, []byte("<< /S /JavaScript /JS (void 0) >>"))
			names := "<< /JavaScript << /Names [(launch) 6 0 R] >> >>"
			namesObject, namesPath := 1, "Catalog.Names.JavaScript"
			if indirect {
				doc.Objs = append(doc.Objs, []byte(names))
				names = "7 0 R"
				namesObject, namesPath = 7, "JavaScript"
			}
			warningEntry(doc, 1, "/PageLabels << /Nums [0 << /S /D >>] >> /OutputIntents [<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (test) >>] /OpenAction 6 0 R /AA << /WC 6 0 R >> /Names "+names)
			ds := warningFindings(t, doc, SelectionRequest{Item: "one"})
			for _, tc := range []struct {
				code diag.Code
				path string
			}{{diag.SourcePageLabelsRemoved, "Catalog.PageLabels"}, {diag.SourceOutputIntentsRemoved, "Catalog.OutputIntents"}} {
				found := warningCode(ds, tc.code)
				if len(found) != 1 {
					t.Fatalf("%s: %+v", tc.code, found)
				}
				warningLocation(t, found[0], 1, 0, tc.path)
			}
			actions := warningCode(ds, diag.SourceDocumentActionsRemoved)
			if len(actions) != 3 {
				t.Fatalf("distinct document dispositions collapsed: %+v", actions)
			}
			expected := map[string]int{"Catalog.OpenAction": 1, "Catalog.AA": 1, namesPath: namesObject}
			for _, d := range actions {
				if d.PDFLocation == nil {
					t.Fatalf("unlocated disposition: %+v", d)
				}
				object, ok := expected[d.PDFLocation.Path]
				if !ok {
					t.Fatalf("unexpected disposition site: %+v; location %+v", d, *d.PDFLocation)
				}
				warningLocation(t, d, object, 0, d.PDFLocation.Path)
				delete(expected, d.PDFLocation.Path)
			}
			if len(expected) != 0 {
				t.Fatalf("missing disposition sites: %v", expected)
			}
		})
	}
}

func TestWarningURIUsesRetainedOccurrenceSites(t *testing.T) {
	for _, tc := range []struct {
		name, target, base string
		orphan             bool
		page               int
		want               bool
	}{
		{"retained", "relative/path", "https://example.invalid/", false, 1, true},
		{"orphan", "relative/path", "https://example.invalid/", true, 1, false},
		{"selected-out", "relative/path", "https://example.invalid/", false, 2, false},
		{"absolute", "https://target.invalid/path", "https://example.invalid/", false, 1, false},
		{"no-base", "relative/path", "", false, 1, false},
		{"empty-base", "relative/path", "", false, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := pdffixture.URILink("uri", tc.target, tc.base, false)
			if tc.name == "empty-base" {
				warningEntry(doc, 1, "/URI << /Base () >>")
			}
			doc.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 7 0 R] >>")
			doc.Objs = append(doc.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"))
			if tc.orphan {
				doc.Objs[2] = bytes.Replace(doc.Objs[2], []byte(" /Annots [6 0 R]"), nil, 1)
			}
			ds := warningFindings(t, doc, SelectionRequest{Item: "selected", Pages: []packet.PageRange{{First: tc.page, Last: tc.page}}})
			found := warningCode(ds, diag.SourceURIBaseRemoved)
			if !tc.want {
				if len(found) != 0 {
					t.Fatalf("unaffected occurrence warned: %+v", found)
				}
				return
			}
			if len(found) != 1 {
				t.Fatalf("retained URI findings: %+v", found)
			}
			warningLocation(t, found[0], 6, 1, "A.URI")
			if !slices.Equal(found[0].Items, []string{"selected"}) {
				t.Fatalf("URI occurrence attribution: %+v", found[0])
			}
		})
	}
}

func warningForm(tag string) *pdffixture.Doc {
	d := pdffixture.Plain(tag)
	d.Objs = append(d.Objs, []byte(fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Tx /T (%s) /V (%s) /Rect [50 50 250 90] /P 3 0 R /F 4 >>", tag, tag)))
	warningEntry(d, 3, "/Annots [6 0 R]")
	warningEntry(d, 1, "/AcroForm 7 0 R")
	d.Objs = append(d.Objs, []byte("<< /Fields [6 0 R] /DA (/Helv 12 Tf 0 g) /Q 0 /DR << /Font << /Helv 5 0 R >> >> /NeedAppearances true >>"))
	return d
}

func TestWarningFormRequestAndActualRenameSourceSites(t *testing.T) {
	first, second := warningForm("FIRST"), warningForm("SECOND")
	for _, doc := range []*pdffixture.Doc{first, second} {
		ds := warningFindings(t, doc, SelectionRequest{Item: "form"})
		found := warningCode(ds, diag.FormAppearanceRegenerationRequested)
		if len(found) != 1 {
			t.Fatalf("appearance request: %+v", found)
		}
		warningLocation(t, found[0], 7, 0, "NeedAppearances")
	}
	dir := t.TempDir()
	files := map[string]SourceFacts{}
	views := map[string]OccurrenceFacts{}
	domain := map[string]packet.SourceFacts{}
	var items []packet.Item
	for i, doc := range []*pdffixture.Doc{first, second} {
		path := filepath.Join(dir, fmt.Sprintf("source-%d.pdf", i))
		if err := doc.WriteFile(path); err != nil {
			t.Fatal(err)
		}
		item := fmt.Sprintf("form-%d", i)
		f, v, ds := Inspect(t.Context(), doc.Bytes(), "", []SelectionRequest{{Item: item}})
		if status := diag.Outcome(ds); status != "ok" || v[0] == nil {
			t.Fatalf("form inspection: %+v", ds)
		}
		files[path] = f
		views[item] = *v[0]
		domain[item] = v[0].SourceFacts
		items = append(items, &packet.SourceItem{Ref: item, Path: path})
	}
	layout, ds := resolvePacket(packet.NewPlan("", nil, nil, packet.PageStyle{}, items), domain)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	assembled, err := Assemble(t.Context(), testAssembly(layout, files, views, nil), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(assembled.Renames[0]) != 0 || len(assembled.Renames[1]) != 1 {
		t.Fatalf("actual mappings: %+v", assembled.Renames)
	}
	rename := assembled.Renames[1][0]
	if rename.From != "SECOND" || rename.To == rename.From {
		t.Fatalf("actual mapping: %+v", rename)
	}
	found := warningCode(assembled.Diagnostics, diag.FormFieldsRenamed)
	if len(found) != 1 {
		t.Fatalf("rename findings: %+v", found)
	}
	warningLocation(t, found[0], 6, 1, "T")
	if !slices.Equal(found[0].Items, []string{"form-1"}) || !strings.Contains(found[0].Detail, rename.From) || !strings.Contains(found[0].Detail, rename.To) {
		t.Fatalf("actual rename attribution: %+v", found[0])
	}
}

func TestWarningAttachmentAndXFASourceSites(t *testing.T) {
	attachment := pdffixture.Attachment("attachment")
	warningEntry(attachment, 1, "/AF [6 0 R]")
	ds := warningFindings(t, attachment, SelectionRequest{Item: "attachment"})
	found := warningCode(ds, diag.SourceAttachmentsRemoved)
	if len(found) != 2 {
		t.Fatalf("distinct attachment collections collapsed: %+v", found)
	}
	paths := map[string]bool{}
	for _, d := range found {
		if d.PDFLocation == nil {
			t.Fatalf("unlocated attachment collection: %+v", d)
		}
		warningLocation(t, d, 1, 0, d.PDFLocation.Path)
		paths[d.PDFLocation.Path] = true
	}
	if !paths["Catalog.AF"] || !paths["Catalog.Names.EmbeddedFiles"] {
		t.Fatalf("attachment collection sites: %v", paths)
	}
	for _, indirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("indirect-form-%t", indirect), func(t *testing.T) {
			d := warningForm("xfa")
			warningEntry(d, 7, "/XFA [(template) 8 0 R]")
			d.Objs = append(d.Objs, []byte("<< /Length 11 >>\nstream\n<template/>\nendstream"))
			object, path := 7, "XFA"
			appearancePath := "NeedAppearances"
			if !indirect {
				d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/AcroForm 7 0 R"), append([]byte("/AcroForm "), d.Objs[6]...), 1)
				object, path, appearancePath = 1, "Catalog.AcroForm.XFA", "Catalog.AcroForm.NeedAppearances"
			}
			ds := warningFindings(t, d, SelectionRequest{Item: "xfa"})
			found := warningCode(ds, diag.SourceXfaRemoved)
			if len(found) != 1 {
				t.Fatalf("XFA disposition: %+v", found)
			}
			warningLocation(t, found[0], object, 0, path)
			found = warningCode(ds, diag.FormAppearanceRegenerationRequested)
			if len(found) != 1 {
				t.Fatalf("appearance request: %+v", found)
			}
			warningLocation(t, found[0], object, 0, appearancePath)
		})
	}
}

func TestWarningDistinctURISitesKeepOriginalSelectedPage(t *testing.T) {
	d := pdffixture.URILink("two URI sites", "relative/path", "https://example.invalid/", false)
	d.Objs[1] = []byte("<< /Type /Pages /Count 2 /Kids [3 0 R 7 0 R] >>")
	d.Objs = append(d.Objs, []byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [8 0 R] >>"), bytes.Replace(d.Objs[5], []byte("/P 3 0 R"), []byte("/P 7 0 R"), 1))
	ds := warningFindings(t, d, SelectionRequest{Item: "second", Pages: []packet.PageRange{{First: 2, Last: 2}}}, SelectionRequest{Item: "whole"})
	found := warningCode(ds, diag.SourceURIBaseRemoved)
	// Inspection preserves each occurrence's evidence; result coalescing may later combine equal source sites.
	count := map[int]int{}
	for _, finding := range found {
		if finding.PDFLocation == nil {
			t.Fatalf("URI witness lacks location: %+v", finding)
		}
		switch finding.PDFLocation.Object {
		case 6:
			warningLocation(t, finding, 6, 1, "A.URI")
			if !slices.Equal(finding.Items, []string{"whole"}) {
				t.Fatalf("selected-out URI attribution: %+v", finding)
			}
		case 8:
			warningLocation(t, finding, 8, 2, "A.URI")
			if !slices.Contains(finding.Items, "second") && !slices.Contains(finding.Items, "whole") {
				t.Fatalf("retained URI attribution: %+v", finding)
			}
		default:
			t.Fatalf("URI witness uses generated/renumbered object: %+v", finding)
		}
		count[finding.PDFLocation.Object] += len(finding.Items)
	}
	if count[6] != 1 || count[8] != 2 {
		t.Fatalf("URI occurrence evidence lost or duplicated: counts=%v findings=%+v", count, found)
	}
}

func TestWarningNavigationSurvivesLaterStructureFitFailure(t *testing.T) {
	doc := taggedSelectionFixture()
	annotation := len(doc.Objs) + 1
	doc.Objs = append(doc.Objs, []byte("<< /Type /Annot /Subtype /FreeText /Rect [50 50 200 100] /Contents (fallback layout) /DA (/F1 12 Tf 0 g) /P 4 0 R >>"))
	warningEntry(doc, 4, fmt.Sprintf("/AA << /O << /S /GoTo /D [6 0 R /Fit] >> >> /Annots [%d 0 R]", annotation))
	for _, fitted := range []bool{false, true} {
		t.Run(fmt.Sprintf("fitted-%t", fitted), func(t *testing.T) {
			request := SelectionRequest{Item: "selected", Pages: []packet.PageRange{{First: 1, Last: 1}}}
			if fitted {
				request.FitTo = &packet.Size{Width: 300, Height: 400}
			}
			_, occurrences, ds := Inspect(t.Context(), doc.Bytes(), "", []SelectionRequest{request})
			navigation := warningCode(ds, diag.SourceNavigationRemoved)
			if len(navigation) != 1 {
				t.Fatalf("established navigation finding lost: %+v", ds)
			}
			warningLocation(t, navigation[0], 4, 1, "AA.O")
			if !slices.Equal(navigation[0].Items, []string{"selected"}) {
				t.Fatalf("navigation occurrence attribution: %+v", navigation[0])
			}
			fitting := warningCode(ds, diag.FitAnnotationUnsupported)
			if fitted {
				if len(fitting) != 1 || occurrences[0] != nil {
					t.Fatalf("expected later annotation fitting failure: findings=%+v occurrence=%+v", ds, occurrences[0])
				}
			} else {
				if status := diag.Outcome(ds); status != "ok" || occurrences[0] == nil || len(fitting) != 0 {
					t.Fatalf("unfitted control: %+v", ds)
				}
			}
		})
	}
}
