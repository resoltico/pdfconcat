// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
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
	plan, err := json.Marshal(map[string]any{"version": 4, "items": []any{map[string]any{"source": source}, map[string]any{"source": source}}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []core.Input{
		core.Items{core.SourceItem{Ref: "/items/0", Path: source}, core.SourceItem{Ref: "/items/1", Path: source}},
		core.PlanBytes{Data: plan, Label: "plan.json"},
	}
	var expected []core.Diagnostic
	for inputIndex, input := range inputs {
		for _, operation := range []core.Operation{core.Check, core.Build} {
			t.Run(fmt.Sprintf("input=%d/%s", inputIndex, operation), func(t *testing.T) {
				output := filepath.Join(t.TempDir(), "output.pdf")
				report := filepath.Join(t.TempDir(), "report.json")
				r := core.Execute(t.Context(), core.Request{Operation: operation, Input: input, WorkingDir: dir, Output: output, Report: report})
				schemaValid(t, r)
				if r.Status != "ok" || len(r.Diagnostics) != 3 || r.Pages.Total == nil || *r.Pages.Total != 2 || r.Output.Published != (operation == core.Build) || !r.Report.Written {
					t.Fatalf("result: %+v", r)
				}
				for i, d := range r.Diagnostics {
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
				var decoded core.Result
				if e = json.Unmarshal(saved, &decoded); e != nil {
					t.Fatal(e)
				}
				if !reflect.DeepEqual(decoded.Diagnostics, r.Diagnostics) {
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
