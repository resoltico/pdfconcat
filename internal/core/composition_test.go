// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func packetPlan(t *testing.T, dir string, value map[string]any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plan.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestCompositionReportPartitionsPhysicalPacket(t *testing.T) {
	dir := t.TempDir()
	tools := oracle.RequireTools(t)
	if err := pdffixture.Pages("SOURCE", 4).WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
		t.Fatal(err)
	}
	recipe := map[string]any{"version": 4, "output": "packet.pdf", "page_defaults": map[string]any{"size": "A4"}, "page_numbers": map[string]any{}, "items": []any{
		map[string]any{"page": map[string]any{"text": map[string]any{"value": "Cover"}}},
		map[string]any{"contents": map[string]any{}},
		map[string]any{"title": "Pielikums Ā Ελληνικά", "start_on": "odd", "divider": map[string]any{}, "items": []any{
			map[string]any{"source": "source.pdf", "pages": [][2]int{{1, 1}, {3, 3}}},
			map[string]any{"title": "Кириллица", "start_on": "odd", "items": []any{map[string]any{"source": "source.pdf", "pages": [][2]int{{2, 2}}}}},
		}},
		map[string]any{"page": map[string]any{}, "count": 2},
	}}
	path := packetPlan(t, dir, recipe)
	checked := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.PlanFile{Path: path}, WorkingDir: dir})
	if checked.Status != "ok" || checked.Output.Published {
		t.Fatalf("check: %+v", checked.Diagnostics)
	}
	built := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanFile{Path: path}, WorkingDir: dir})
	if built.Status != "ok" || !built.Output.Published {
		t.Fatalf("build: %+v", built.Diagnostics)
	}
	encoded, err := json.Marshal(built.Full())
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Parts []map[string]any `json:"parts"`
	}
	if err = json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, part := range wire.Parts {
		if part["kind"] == "source" {
			spans, ok := part["spans"].([]any)
			if !ok || len(spans) == 0 {
				t.Fatalf("known source mapping was lost: %s", encoded)
			}
		} else if part["spans"] != nil {
			t.Fatalf("non-source mapping must be null: %s", encoded)
		}
	}
	if !reflect.DeepEqual(checked.Pages, built.Pages) || !reflect.DeepEqual(checked.Sections, built.Sections) {
		t.Fatal("check/build physical facts differ")
	}
	if len(built.Sections) != 2 || len(built.Parts) < 6 {
		t.Fatalf("missing composition records: %+v", built)
	}
	next := int64(1)
	var sourcePages int64
	for _, part := range built.Parts {
		if part.OutputPages == nil || part.Pages == nil || part.OutputPages[0] != next || part.OutputPages[1] != next+*part.Pages-1 {
			t.Fatalf("leaf partition broken: %+v", part)
		}
		next = part.OutputPages[1] + 1
		if part.Kind == "source" {
			if part.InputPages == nil || *part.InputPages != 4 {
				t.Fatal("lost full-input count")
			}
			sourcePages += *part.Pages
			spanNext := part.OutputPages[0]
			for _, span := range *part.Spans {
				if span.Output[0] != spanNext || span.Output[1]-span.Output[0] != int64(span.Source[1]-span.Source[0]) {
					t.Fatal("mapping mismatch")
				}
				spanNext = span.Output[1] + 1
			}
			if spanNext != next {
				t.Fatal("mapping does not cover part")
			}
		}
	}
	if next != *built.Pages.Total+1 || sourcePages != 3 || *built.Pages.Generated+sourcePages != *built.Pages.Total {
		t.Fatal("page totals disagree with partition")
	}
	doc, err := oracle.Load(tools, built.Output.Path)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := doc.OutlineTargets()
	if err != nil || len(targets) != 2 {
		t.Fatalf("authored outlines: %v %v", targets, err)
	}
	for i, section := range built.Sections {
		if section.TargetPage == nil || targets[i] != int(*section.TargetPage) {
			t.Fatalf("outline/report target mismatch: %+v", section)
		}
	}
	raw, err := exec.CommandContext(t.Context(), tools.PDFToText, "-raw", built.Output.Path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	texts := strings.Split(strings.TrimSuffix(string(raw), "\f"), "\f")
	for _, part := range built.Parts {
		for page := part.OutputPages[0]; page <= part.OutputPages[1]; page++ {
			text := strings.TrimSpace(texts[page-1])
			if part.Kind == "padding" {
				if text != "" {
					t.Fatalf("padding painted: %q", text)
				}
			} else if !strings.HasSuffix(text, strconv.FormatInt(page, 10)) {
				t.Fatalf("missing physical footer%d: %q", page, text)
			}
		}
		if part.Kind == "contents" {
			links, e := doc.ActionTargets(int(part.OutputPages[0]))
			if e != nil || !reflect.DeepEqual(links, targets) {
				t.Fatalf("contents/report/outline targets disagree: %v %v", links, e)
			}
		}
	}
	if err = tools.Check(built.Output.Path); err != nil {
		t.Fatal(err)
	}
}

func TestRefusedCompositionRetainsOnlyEstablishedFacts(t *testing.T) {
	for _, kind := range []string{"selection", "font", "layout"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if err := pdffixture.Pages("SOURCE", 3).WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
				t.Fatal(err)
			}
			source := map[string]any{"source": "source.pdf"}
			page := map[string]any{"size": "A4"}
			switch kind {
			case "selection":
				source["pages"] = [][2]int{{4, 4}}
			case "font":
				page["text"] = map[string]any{"font": map[string]any{"file": "missing.ttf"}}
			case "layout":
				// Declared count is valid; the derived odd-start blank exceeds the actual generated limit.
			}
			count := 1
			if kind == "layout" {
				count = 100000
			}
			recipe := map[string]any{"version": 4, "output": "packet.pdf", "items": []any{source, map[string]any{"title": "Section", "start_on": "odd", "items": []any{map[string]any{"page": page, "count": count}}}}}
			path := packetPlan(t, dir, recipe)
			report := filepath.Join(dir, "report.json")
			if kind == "font" {
				report = filepath.Join(t.TempDir(), "report.json")
			}
			r := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanFile{Path: path}, WorkingDir: dir, Report: report})
			if r.Status == "ok" || r.Output.Published || !r.Report.Written {
				t.Fatalf("refusal/report: %+v", r)
			}
			if len(r.Sections) != 1 || len(r.Parts) < 2 {
				t.Fatal("declared metadata disappeared")
			}
			if kind == "selection" {
				if r.Parts[0].InputPages == nil || *r.Parts[0].InputPages != 3 || r.Parts[0].SHA256 == "" {
					t.Fatal("readable input facts lost")
				}
			}
			{
				if r.Pages.Total != nil || r.Sections[0].TargetPage != nil || r.Sections[0].OutputPages != nil {
					t.Fatal("unresolved final facts invented")
				}
				for _, p := range r.Parts {
					if p.Kind == "padding" || p.OutputPages != nil || p.Pages != nil || p.Spans != nil {
						t.Fatalf("unresolved emission predicted: %+v", p)
					}
				}
			}
			saved, err := os.ReadFile(report)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Parts []map[string]any `json:"parts"`
			}
			if err = json.Unmarshal(saved, &wire); err != nil {
				t.Fatal(err)
			}
			for _, part := range wire.Parts {
				spans, present := part["spans"]
				if !present || spans != nil {
					t.Fatalf("unknown/not-applicable mapping must serialize as null: %s", saved)
				}
			}
			var decoded result.Result
			if err = json.Unmarshal(saved, &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Sections) != 1 {
				t.Fatal("report lost declarations")
			}
		})
	}
}
