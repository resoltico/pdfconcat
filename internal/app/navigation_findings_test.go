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
