// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	exchange "github.com/resoltico/pdfconcat/internal/wire"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestViewerSettingFindingsSurviveCorePlansBuildsAndReports(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	doc := pdffixture.Plain("viewer")
	doc.Objs[0] = bytes.Replace(doc.Objs[0], []byte("/Type /Catalog"), []byte("/Type /Catalog /PageLayout /PDLayoutDontCare /PageMode /Unknown /ViewerPreferences (invalid)"), 1)
	data := doc.Bytes()
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := json.Marshal(map[string]any{"version": 7, "items": []any{map[string]any{"source": source}, map[string]any{"source": source}}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []app.Input{
		app.Items{app.SourceItem{Ref: "/items/0", Path: source}, app.SourceItem{Ref: "/items/1", Path: source}},
		app.PlanBytes{Data: plan, Label: "plan.json"},
	}
	var expected []app.Diagnostic
	for inputIndex, input := range inputs {
		for _, operation := range []app.Operation{app.Check, app.Build} {
			t.Run(fmt.Sprintf("input=%d/%s", inputIndex, operation), func(t *testing.T) {
				output := filepath.Join(t.TempDir(), "output.pdf")
				report := filepath.Join(t.TempDir(), "report.json")
				r := app.Execute(t.Context(), app.Request{InspectionWorkers: 1, Operation: operation, Input: input, WorkingDir: dir, Output: output, Report: report})
				schemaValid(t, r)
				if r.Status != "ok" || len(r.Diagnostics) != 4 || r.Pages.Total == nil || *r.Pages.Total != 2 || r.Output.Published != (operation == app.Build) || !r.Report.Written {
					t.Fatalf("result: %+v", r)
				}
				for i, d := range r.Diagnostics {
					if d.Code.String() == "source_structure_partial" {
						if i != 3 {
							t.Fatal("coverage finding changed source viewer order")
						}
						continue
					}
					if d.Code.String() != "source_viewer_settings_removed" || d.File != source || d.PDFLocation == nil || !reflect.DeepEqual(d.Items, []string{"/items/0", "/items/1"}) {
						t.Fatalf("finding %d: %+v", i, d)
					}
				}
				if expected == nil {
					expected = r.Diagnostics
				} else if !reflect.DeepEqual(expected, r.Diagnostics) {
					t.Fatal("input or operation changed findings", r.Diagnostics)
				}
				for _, part := range r.Parts {
					if part.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
						t.Fatal("report no longer identifies original bytes", part)
					}
				}
				saved, e := os.ReadFile(report)
				if e != nil {
					t.Fatal(e)
				}
				var decoded exchange.Result
				if e = json.Unmarshal(saved, &decoded); e != nil {
					t.Fatal(e)
				}
				encodedActual, e := json.Marshal(r.Diagnostics)
				if e != nil {
					t.Fatal(e)
				}
				encodedSaved, e := json.Marshal(decoded.Diagnostics)
				if e != nil {
					t.Fatal(e)
				}
				if !bytes.Equal(encodedActual, encodedSaved) {
					t.Fatal("saved report lost findings")
				}
				original, e := os.ReadFile(source)
				if e != nil || !bytes.Equal(data, original) {
					t.Fatal("source changed", e)
				}
			})
		}
	}
}
