// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
)

func titlePlan(t *testing.T, sections int, contents, dividers bool, font string) string {
	t.Helper()
	items := []any{}
	if contents {
		items = append(items, map[string]any{"contents": map[string]any{}})
	}
	for range sections {
		section := map[string]any{"title": "  漢字  ", "items": []any{map[string]any{"page": map[string]any{}}}}
		if dividers {
			section["divider"] = map[string]any{}
		}
		items = append(items, section)
	}
	page := map[string]any{"size": "A4"}
	if font != "" {
		page["text"] = map[string]any{"font": map[string]any{"file": font}}
	}
	data, err := json.Marshal(map[string]any{"version": 3, "page": page, "items": items})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPaintedTitleDiagnosticsRetainDeclarationAndConsumers(t *testing.T) {
	for _, tc := range []struct {
		name               string
		contents, dividers bool
		want               []string
	}{
		{"contents", true, false, []string{"/items/1/title", "/items/2/title", "/items/0"}},
		{"dividers", false, true, []string{"/items/0/title", "/items/1/title", "/items/0/divider", "/items/1/divider"}},
		{"both", true, true, []string{"/items/1/title", "/items/2/title", "/items/0", "/items/1/divider", "/items/2/divider"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			report := filepath.Join(dir, "report.json")
			input := titlePlan(t, 2, tc.contents, tc.dividers, "")
			summary, exit, _ := invoke(t, []string{"check", "--plan", "-", "--base-dir", dir, "--report", report}, input)
			if exit != 2 || len(summary.Diagnostics) != 1 || summary.Diagnostics[0].Code.String() != "text_unsupported_character" {
				t.Fatalf("painted title refusal: %+v %d", summary, exit)
			}
			items := summary.Diagnostics[0].Items
			for _, want := range tc.want {
				if !slices.Contains(items, want) {
					t.Fatalf("missing declaration/consumer %s in serialized stdout: %+v", want, items)
				}
			}
			if len(items) != len(tc.want) {
				t.Fatalf("duplicate/unrelated title pointers: %+v", items)
			}
			data, err := os.ReadFile(report)
			if err != nil {
				t.Fatal(err)
			}
			validateResult(t, data)
			var full core.Result
			if err = json.Unmarshal(data, &full); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(full.Diagnostics[0].Items, items) || full.Sections[0].Title != "  漢字  " || full.Sections[1].Title != "  漢字  " {
				t.Fatalf("report changed title/origin values: %+v", full)
			}
		})
	}
}

func TestTitleDiagnosticCapsAndFontOriginInSerializedReports(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.json")
	input := titlePlan(t, 25, true, true, "")
	summary, exit, _ := invoke(t, []string{"check", "--plan", "-", "--base-dir", dir, "--report", report}, input)
	if exit != 2 || len(summary.Diagnostics) != 1 || len(summary.Diagnostics[0].Items) != 20 || summary.Diagnostics[0].ItemsOmitted != 31 {
		t.Fatalf("title consumer caps: %+v %d", summary, exit)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, data)
	var full core.Result
	if err = json.Unmarshal(data, &full); err != nil {
		t.Fatal(err)
	}
	items := full.Diagnostics[0].Items
	if len(items) != 51 || full.Diagnostics[0].ItemsOmitted != 0 {
		t.Fatalf("uncapped report origins: %+v", full.Diagnostics)
	}
	for i := range 25 {
		for _, suffix := range []string{"/title", "/divider"} {
			want := fmt.Sprintf("/items/%d%s", i+1, suffix)
			if !slices.Contains(items, want) {
				t.Fatalf("full report lost %s", want)
			}
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--plan", "-", "--base-dir", dir, "--format", "text"}, strings.NewReader(input), &stdout, &stderr); code != 2 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "31 more; use --report") {
		t.Fatalf("human title omission hint: %d %q %q", code, stdout.String(), stderr.String())
	}
	fontReport := filepath.Join(t.TempDir(), "font-report.json")
	missing := "missing-Ā-Ελληνικά.ttf"
	summary, exit, _ = invoke(t, []string{"check", "--plan", "-", "--base-dir", dir, "--report", fontReport}, titlePlan(t, 2, false, true, missing))
	if exit != 1 || len(summary.Diagnostics) != 1 || summary.Diagnostics[0].Code.String() != "font_unreadable" {
		t.Fatalf("selected-font refusal: %+v %d", summary, exit)
	}
	for _, want := range []string{"/page/text/font", "/items/0/title", "/items/0/divider", "/items/1/title", "/items/1/divider"} {
		if !slices.Contains(summary.Diagnostics[0].Items, want) {
			t.Fatalf("font error lost %s: %+v", want, summary.Diagnostics)
		}
	}
	data, err = os.ReadFile(fontReport)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, data)
	if err = json.Unmarshal(data, &full); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(full.Diagnostics[0].Items, summary.Diagnostics[0].Items) || full.Diagnostics[0].File != filepath.Join(dir, missing) {
		t.Fatalf("serialized font consumers/file changed: %+v", full.Diagnostics)
	}
}

func TestBookmarkOnlyTitleDoesNotRequireGlyphValidation(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.json")
	r, exit, _ := invoke(t, []string{"check", "--plan", "-", "--base-dir", dir, "--report", report}, titlePlan(t, 2, false, false, ""))
	if exit != 0 || r.Status != "ok" || len(r.Diagnostics) != 0 {
		t.Fatalf("unpainted title incorrectly shaped: %+v %d", r, exit)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	validateResult(t, data)
	var full core.Result
	if err = json.Unmarshal(data, &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Sections) != 2 || full.Sections[0].Title != "  漢字  " {
		t.Fatalf("bookmark title lost: %+v", full.Sections)
	}
}
