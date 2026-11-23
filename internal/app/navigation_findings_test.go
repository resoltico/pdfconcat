// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestNavigationFindingsReconcileAcrossAssemblyBeforeCaps(t *testing.T) {
	dir := t.TempDir()
	links, form := filepath.Join(dir, "links.pdf"), filepath.Join(dir, "form.pdf")
	doc := pdffixture.URILink("relative", "one", "https://example.invalid/", false)
	doc.Objs[5] = bytes.Replace(doc.Objs[5], []byte("/IsMap false"), []byte("/IsMap false /Next << /S /URI /URI (two) >>"), 1)
	if err := doc.WriteFile(links); err != nil {
		t.Fatal(err)
	}
	if err := pdffixture.Form("form").WriteFile(form); err != nil {
		t.Fatal(err)
	}
	items := []packet.Item{
		&packet.SourceItem{Ref: "first", Path: links}, &packet.SourceItem{Ref: "second", Path: links},
		&packet.SourceItem{Ref: "form", Path: form}, &packet.SourceItem{Ref: "repeat-form", Path: form},
	}
	for _, op := range []app.Operation{app.Check, app.Build} {
		result := app.Execute(t.Context(), app.Request{
			Operation: op, WorkingDir: dir, InspectionWorkers: 1, Output: filepath.Join(dir, "output.pdf"),
			Input: app.Declared{Declaration: packet.Declaration{Items: items}},
		})
		if result.Status != "ok" {
			t.Fatal(result.Diagnostics)
		}
		sites := map[string]bool{}
		renamed := false
		for _, finding := range result.Diagnostics {
			switch finding.Code {
			case diag.SourceURIBaseRemoved:
				if finding.File != links || finding.PDFLocation == nil || !slices.Equal(finding.Items, []string{"first", "second"}) ||
					strings.Contains(finding.Detail, "Assembly") != (op == app.Build) || sites[finding.PDFLocation.Path] {
					t.Fatal(finding)
				}
				sites[finding.PDFLocation.Path] = true
			case diag.FormFieldsRenamed:
				renamed = renamed || finding.Identity.Phase == "assembly"
			}
		}
		if len(sites) != 2 || !sites["A.URI"] || !sites["A.Next.URI"] || renamed != (op == app.Build) {
			t.Fatal(result.Diagnostics)
		}
	}
}

func TestExternalActionFindingsKeepOriginalNextSlot(t *testing.T) {
	for _, tc := range []struct {
		name, catalog string
		code          diag.Code
	}{
		{"implicit base", "", diag.SourceRelativeActionContext},
		{"explicit base", " /URI << /Base (https://example.invalid/source/) >>", diag.SourceURIBaseRemoved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "source.pdf")
			d := pdffixture.NavigationPages("projection", false, false)
			d.Objs[0] = bytes.Replace(d.Objs[0], []byte("/Pages 2 0 R"), []byte("/Pages 2 0 R"+tc.catalog), 1)
			ref := len(d.Objs) - 1
			d.Objs[ref] = bytes.Replace(d.Objs[ref], []byte("/Dest [6 0 R /Fit]"), []byte("/A << /S /URI /URI (https://example.invalid/) /Next [<< /S /GoTo /D [6 0 R /Fit] >> << /S /URI /URI (missing-annex.pdf) >>] >>"), 1)
			if err := d.WriteFile(source); err != nil {
				t.Fatal(err)
			}
			selection := []packet.PageRange{{First: 1, Last: 1}}
			items := []packet.Item{&packet.SourceItem{Ref: "first", Path: source, Selection: selection}, &packet.SourceItem{Ref: "again", Path: source, Selection: selection}}
			for _, op := range []app.Operation{app.Check, app.Build} {
				request := app.Request{Operation: op, WorkingDir: dir, InspectionWorkers: 1, Input: app.Declared{Declaration: packet.Declaration{Items: items}}}
				if op == app.Build {
					request.Output = "packet.pdf"
				}
				r := app.Execute(t.Context(), request)
				if r.Status != "ok" {
					t.Fatal(r.Diagnostics)
				}
				matches := 0
				for _, d := range r.Diagnostics {
					if d.Code != diag.SourceURIBaseRemoved && d.Code != diag.SourceRelativeActionContext {
						continue
					}
					matches++
					if d.Code != tc.code || d.File != source || d.PDFLocation == nil || d.PDFLocation.Page != 1 ||
						d.PDFLocation.Path != "A.Next[1].URI" || !slices.Equal(d.Items, []string{"first", "again"}) ||
						strings.Contains(d.Detail, "Assembly") != (op == app.Build) {
						t.Fatalf("%s action provenance: %+v", op, d)
					}
				}
				if matches != 1 {
					t.Fatalf("%s findings: %v", op, r.Diagnostics)
				}
			}
		})
	}
}
