// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
)

func TestEmergencyWrappingMetadataPaths(t *testing.T) {
	id := "A" + strings.Repeat("1", 47)
	for _, name := range []string{"re\u0301sume\u0301.txt", strings.Repeat("x", 220) + ".txt"} {
		t.Run(name[:2], func(t *testing.T) {
			dir := t.TempDir()
			pathText := "originals/data/Supporting/" + id + "/original/" + name
			cover := strings.Join([]string{"Annex " + id, "Invoice original", "", "Public note: original bytes.", "Second line with spaces.", "", "Origin: Local synthetic author", "Original file: " + pathText, "Original SHA-256: " + strings.Repeat("a", 64), "The original is not a PDF and has no reading copy; open the original file."}, "\n")
			text := map[string]any{"value": cover, "size": 11, "anchor": "top-left", "x": 36, "y": -48, "align": "left"}
			recipe := map[string]any{"version": 5, "output": "packet.pdf", "page_defaults": map[string]any{"size": "A4", "text": text}, "items": []any{map[string]any{"page": map[string]any{}}}}
			path := packetPlan(t, dir, recipe)
			for _, operation := range []core.Operation{core.Check, core.Build} {
				r := core.Execute(t.Context(), core.Request{Operation: operation, Input: core.PlanFile{Path: path}, WorkingDir: dir})
				if r.Status != "invalid" || r.Output.Published || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code.String() != "text_overflow" {
					t.Fatalf("default wrapping must refuse: %+v", r)
				}
			}
			text["wrap"] = "emergency"
			path = packetPlan(t, dir, recipe)
			checked := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanFile{Path: path}, WorkingDir: dir})
			built := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanFile{Path: path}, WorkingDir: dir})
			if checked.Status != "ok" || built.Status != "ok" || !built.Output.Published || len(built.Parts) != 1 || built.Parts[0].Text == nil || built.Parts[0].Text.Overflow {
				t.Fatalf("emergency check/build: %+v %+v", checked.Diagnostics, built.Diagnostics)
			}
			if checked.Parts[0].Text.Bounds != built.Parts[0].Text.Bounds {
				t.Fatal("check and build disagree about wrapped geometry")
			}
			t.Run("extraction", func(t *testing.T) {
				tools := oracle.RequireTools(t)
				doc, err := oracle.Load(tools, built.Output.Path)
				if err != nil {
					t.Fatal(err)
				}
				if findings := doc.Verify(oracle.Expectation{Pages: []oracle.ExpectedPage{{Text: "Annex " + id}}}); len(findings) != 0 {
					t.Fatal(findings)
				}
				raw, err := exec.CommandContext(t.Context(), tools.PDFToText, "-raw", built.Output.Path, "-").Output()
				if err != nil {
					t.Fatal(err)
				}
				// Only visual line separators are ignored. Do not normalize Unicode or
				// strip arbitrary whitespace: every path character must survive exactly.
				got := strings.ReplaceAll(strings.TrimRight(string(raw), "\n\f"), "\n", "")
				want := strings.ReplaceAll(strings.Replace(cover, "Original file: ", "Original file:", 1), "\n", "")
				if got != want {
					t.Fatalf("path extraction: %q", got)
				}
			})
		})
	}
}

func TestEmergencyWrappingDividerAndExplicitReset(t *testing.T) {
	dir := t.TempDir()
	title := strings.Repeat("identifier", 15)
	defaults := map[string]any{"size": "A4", "text": map[string]any{"wrap": "emergency"}}
	recipe := map[string]any{"version": 5, "page_defaults": defaults, "items": []any{
		map[string]any{"title": title, "divider": map[string]any{}, "items": []any{map[string]any{"page": map[string]any{}}}},
	}}
	path := packetPlan(t, dir, recipe)
	r := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanFile{Path: path}, WorkingDir: dir})
	if r.Status != "ok" || len(r.Parts) != 2 || r.Parts[0].Kind != "divider" || r.Parts[0].Text == nil || r.Parts[0].Text.Overflow {
		t.Fatalf("divider must inherit emergency wrapping: %+v", r)
	}
	recipe["items"] = []any{map[string]any{"page": map[string]any{"text": map[string]any{"value": title, "wrap": "word"}}}}
	path = packetPlan(t, dir, recipe)
	r = core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanFile{Path: path}, WorkingDir: dir})
	if r.Status != "invalid" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code.String() != "text_overflow" {
		t.Fatalf("explicit word reset must override emergency default: %+v", r)
	}
}
