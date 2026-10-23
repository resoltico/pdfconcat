// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

type corpusPolicy struct {
	ID    string
	Items []any
	Fit   string
	Code  string
	Pages int
}

func corpusSource(name string, ranges ...[2]int) map[string]any {
	r := map[string]any{"source": name}
	if len(ranges) > 0 {
		r["pages"] = ranges
	}
	return r
}
func corpusPolicies(t *testing.T) {
	cases := []corpusPolicy{
		{"approval-repeat", []any{corpusSource("approval-form.pdf"), corpusSource("approval-form.pdf")}, "", "", 4},
		{"forms-whole-explicit", []any{corpusSource("form.pdf", [2]int{1, 2})}, "", "", 2},
		{"forms-partial-widget-free", []any{corpusSource("approval-form.pdf", [2]int{2, 2})}, "", "source_selection_form_unsupported", 0},
		{"forms-repeat-namespaces", []any{corpusSource("form.pdf"), corpusSource("form.pdf")}, "", "", 4},
		{"forms-default-conflict", []any{corpusSource("helvetica.pdf"), corpusSource("courier.pdf")}, "", "source_form_unsupported", 0},
		{"forms-fit", []any{corpusSource("form.pdf")}, "A4", "fit_form_unsupported", 0},
		{"pdf2-missing-first", []any{corpusSource("helvetica.pdf"), corpusSource("v2.pdf")}, "", "source_output_incompatible", 0},
		{"pdf2-missing-last", []any{corpusSource("v2.pdf"), corpusSource("helvetica.pdf")}, "", "source_output_incompatible", 0},
		{"pdf2-complete-first", []any{corpusSource("complete.pdf"), corpusSource("v2.pdf")}, "", "", 2},
		{"pdf2-complete-last", []any{corpusSource("v2.pdf"), corpusSource("complete.pdf")}, "", "", 2},
		{"pdf2-link-exempt", []any{corpusSource("uri.pdf"), corpusSource("v2.pdf")}, "", "", 2},
		{"pdf2-omitted-note", []any{corpusSource("note.pdf", [2]int{2, 2}), corpusSource("v2.pdf")}, "", "", 2},
		{"pdf2-retained-note", []any{corpusSource("note.pdf", [2]int{1, 1}), corpusSource("v2.pdf")}, "", "source_output_incompatible", 0},
		{"fit-numeric-direct", []any{corpusSource("numeric.pdf")}, "A4", "fit_destination_unsupported", 0},
		{"fit-numeric-indirect", []any{corpusSource("numeric-indirect.pdf")}, "A4", "fit_destination_unsupported", 0},
		{"fit-omitted-numeric", []any{corpusSource("numeric.pdf", [2]int{1, 1})}, "A4", "", 1},
		{"fit-whole-target", []any{corpusSource("whole-target.pdf")}, "A4", "", 2},
		{"refusal-before-missing-output-parent", []any{corpusSource("form.pdf")}, "A4", "fit_form_unsupported", 0},
	}
	var records []corpusRecord
	for _, tc := range cases {
		t.Run(tc.ID, func(t *testing.T) {
			dir := corpusStage(t, "approval-form.pdf")
			fixtures := map[string]*pdffixture.Doc{"form.pdf": pdffixture.Form("FORM"), "helvetica.pdf": pdffixture.StaticForm("HEL", "Helvetica", false), "courier.pdf": pdffixture.StaticForm("COURIER", "Courier", false), "complete.pdf": pdffixture.StaticForm("COMPLETE", "Helvetica", true), "v2.pdf": pdffixture.Versioned("V2", "2.0", ""), "uri.pdf": pdffixture.URILink("URI", "https://example.invalid/fixture", "", false), "note.pdf": pdffixture.AnnotationPages("NOTE"), "numeric.pdf": pdffixture.NavigationPages("NUMERIC", true, false), "numeric-indirect.pdf": pdffixture.NavigationPages("NUMERIC INDIRECT", true, true), "whole-target.pdf": pdffixture.NavigationPages("WHOLE", false, false)}
			for name, fixture := range fixtures {
				if err := fixture.WriteFile(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			plan := map[string]any{"version": 3, "output": "packet.pdf", "items": tc.Items}
			if tc.Fit != "" {
				plan["fit_to"] = tc.Fit
			}
			if tc.ID == "refusal-before-missing-output-parent" {
				plan["output"] = "absent/packet.pdf"
			}
			path := filepath.Join(dir, "plan.json")
			corpusJSON(t, path, plan)
			for _, operation := range []core.Operation{core.Check, core.Build} {
				r := core.Execute(t.Context(), core.NewRequest(operation, core.FromPlan(core.FilePlan(path)), core.Options{WorkingDir: dir, Report: filepath.Join(dir, string(operation)+".json")}))
				schemaValid(t, r)
				corpusJSON(t, filepath.Join(dir, string(operation)+"-complete.json"), r)
				if tc.Code != "" {
					if !code(r, tc.Code) || r.Output.Published || core.ExitCode(r) != 2 {
						t.Fatal(r)
					}
					if code(r, "output_write_failed") {
						t.Fatal("unreached output failure fabricated", r)
					}
					if _, err := os.Stat(r.Output.Path); err == nil {
						t.Fatal("refused output exists")
					}
					continue
				}
				if r.Status != "ok" || *r.Pages.Total != int64(tc.Pages) {
					t.Fatal(r)
				}
				if operation == core.Check {
					if r.Output.Published {
						t.Fatal("check published")
					}
					continue
				}
				corpusArtifact(t, r)
				if tc.ID == "forms-repeat-namespaces" || tc.ID == "approval-repeat" {
					if len(r.Parts[1].FieldsRenamed) == 0 {
						t.Fatal("actual form rename facts missing")
					}
				}
				t.Run("oracle", func(t *testing.T) {
					tools := oracle.RequireTools(t)
					if err := tools.Check(r.Output.Path); err != nil {
						t.Fatal(err)
					}
					doc, err := oracle.Load(tools, r.Output.Path)
					if err != nil || doc.PageCount() != tc.Pages {
						t.Fatalf("physical count: %v %v", doc, err)
					}
					if strings.HasPrefix(tc.ID, "pdf2-") && doc.EffectiveVersion() != 20 {
						t.Fatal("whole-source version not preserved")
					}
					if tc.ID == "fit-omitted-numeric" {
						targets, err := doc.ActionTargets(1)
						if err != nil || len(targets) != 0 {
							t.Fatal("omitted target survived", targets, err)
						}
					}
				})
			}
			records = append(records, corpusRecord{ID: tc.ID, Boundary: "core check/build refusal or publication", ArtifactDir: dir})
		})
	}
	t.Run("cff-full-and-pruned", func(t *testing.T) {
		dir := corpusStage(t, "technical-note.pdf")
		data, err := os.ReadFile("internal/typeset/fontdata/NotoSans-Regular.ttf")
		if err != nil {
			t.Fatal(err)
		}
		full := bytes.Clone(data)
		for n := range int(binary.BigEndian.Uint16(full[4:])) {
			rec := full[12+n*16:]
			if string(rec[:4]) == "OS/2" {
				offset := int(binary.BigEndian.Uint32(rec[8:]))
				binary.BigEndian.PutUint16(full[offset+8:], 0x100)
			}
		}
		for name, font := range map[string][]byte{"pruned.ttf": data, "full.ttf": full} {
			if err = os.WriteFile(filepath.Join(dir, name), font, 0600); err != nil {
				t.Fatal(err)
			}
		}
		plan := map[string]any{"version": 3, "output": "packet.pdf", "page": map[string]any{"size": "A4"}, "items": []any{corpusSource("technical-note.pdf"), corpusPage("Pruned Ā Ελληνικά", "pruned.ttf"), corpusPage("Full Ā Ελληνικά", "full.ttf")}}
		path := filepath.Join(dir, "plan.json")
		corpusJSON(t, path, plan)
		r := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromPlan(core.FilePlan(path)), core.Options{WorkingDir: dir}))
		if r.Status != "ok" || *r.Pages.Total != 3 {
			t.Fatal(r)
		}
		corpusArtifact(t, r)
		t.Run("oracle", func(t *testing.T) {
			tools := oracle.RequireTools(t)
			doc, err := oracle.Load(tools, r.Output.Path)
			if err != nil {
				t.Fatal(err)
			}
			text, err := doc.RawTexts()
			if err != nil || !strings.Contains(text[1], "Pruned Ā") || !strings.Contains(text[2], "Full Ā") {
				t.Fatal(text, err)
			}
		})
	})
	t.Run("late-report-after-pdf", func(t *testing.T) {
		dir := corpusStage(t, "approval-form.pdf")
		r := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromItems(core.Source("approval-form.pdf", "form")), core.Options{WorkingDir: dir, Output: filepath.Join(dir, "packet.pdf"), Report: filepath.Join(dir, "absent/report.json")}))
		if r.Status != "failed" || !r.Output.Published || r.Report.Written || !code(r, "report_write_failed") {
			t.Fatal(r)
		}
		corpusArtifact(t, r)
	})
	corpusJSON(t, filepath.Join(t.ArtifactDir(), "matrix.json"), records)
}
func TestCorpusNormalPolicyTable(t *testing.T) { corpusPolicies(t) }
