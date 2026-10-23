// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func writePlan(tb testing.TB, dir string, items []any) string {
	tb.Helper()
	data, err := json.Marshal(map[string]any{"version": 4, "output": "packet.pdf", "items": items})
	if err != nil {
		tb.Fatal(err)
	}
	path := filepath.Join(dir, "plan.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestScale(t *testing.T) {
	if os.Getenv("PDFCONCAT_SCALE") != "1" {
		t.Skip("opt-in local scale run: set PDFCONCAT_SCALE=1")
	}
	t.Run("source-and-generated", scaleSourceAndGenerated)
	t.Run("corpus-profiles", corpusScaleProfiles)
	t.Run("staged-build-sigint", corpusStagedBuildSIGINT)
}
func scaleSourceAndGenerated(t *testing.T) {
	tools := oracle.RequireTools(t)
	t.Logf("platform=%s/%s go=%s", runtime.GOOS, runtime.GOARCH, runtime.Version())
	for _, scenario := range []struct {
		name                      string
		sources, generated, count int
		pages                     int64
	}{
		{name: "2000 sources and 2000 generated items", sources: 2000, generated: 2000, count: 1, pages: 6000},
		{name: "one generated item with count 100000", count: 100000, pages: 100000},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixtureStart := time.Now()
			dir := t.TempDir()
			items := make([]any, 0, scenario.sources+scenario.generated+1)
			var inputBytes int64
			for i := range scenario.sources {
				name := fmt.Sprintf("source-%04d.pdf", i)
				data := pdffixture.Pages(fmt.Sprintf("SOURCE %04d", i), 2).Bytes()
				inputBytes += int64(len(data))
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
				items = append(items, map[string]any{"source": name}, map[string]any{"page": map[string]any{"text": map[string]any{"value": fmt.Sprintf("DIVIDER %04d", i)}}})
			}
			if scenario.sources == 0 {
				items = append(items, map[string]any{"page": map[string]any{"size": "Letter"}, "count": scenario.count})
			}
			plan := writePlan(t, dir, items)
			fixtureTime := time.Since(fixtureStart)
			start := time.Now()
			r := core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanFile{Path: plan}, WorkingDir: dir})
			elapsed := time.Since(start)
			if r.Status != "ok" || r.Output == nil || !r.Output.Published || r.Pages == nil || r.Pages.Total == nil || *r.Pages.Total != scenario.pages {
				t.Fatalf("scale outcome: %+v", r)
			}
			actual, err := exec.CommandContext(t.Context(), tools.QPDF, "--show-npages", r.Output.Path).Output()
			if err != nil {
				t.Fatal(err)
			}
			pages, err := strconv.ParseInt(strings.TrimSpace(string(actual)), 10, 64)
			if err != nil || pages != scenario.pages {
				t.Fatalf("independent scale count %q, want %d: %v", actual, scenario.pages, err)
			}
			t.Logf("fixture=%s build_verify_publish=%s source_bytes=%d output_bytes=%d pages=%d", fixtureTime, elapsed, inputBytes, *r.Output.Bytes, pages)
		})
	}
}

func BenchmarkBuild(b *testing.B) {
	dir := b.TempDir()
	source := filepath.Join(dir, "source.pdf")
	if err := pdffixture.Plain("benchmark").WriteFile(source); err != nil {
		b.Fatal(err)
	}
	plan := writePlan(b, dir, []any{map[string]any{"source": "source.pdf"}, map[string]any{"page": map[string]any{"text": map[string]any{"value": "Pielikums Ā ļ ķ Ελληνικά"}}}})
	request := core.Request{Operation: core.Build, Input: core.PlanFile{Path: plan}, WorkingDir: dir, Overwrite: true}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r := core.Execute(b.Context(), request)
		if r.Status != "ok" || !r.Output.Published {
			b.Fatalf("build: %+v", r.Diagnostics)
		}
	}
}

func corpusScaleProfiles(t *testing.T) {
	cli := corpusBinary(t, "pdfconcat-scale", "../../cmd/pdfconcat")
	for _, tc := range []struct {
		ID                          string
		Count                       int
		Numbers, Sections, Repeated bool
	}{{"generated-1000-blank", 1000, false, false, false}, {"generated-1000-numbered", 1000, true, false, false}, {"generated-10000-numbered", 10000, true, false, false}, {"generated-100000-blank", 100000, false, false, false}, {"many-sections", 500, true, true, false}, {"repeated-selected-sources", 500, false, false, true}} {
		t.Run(tc.ID, func(t *testing.T) {
			dir := t.ArtifactDir()
			items := []any{corpusGenerated(tc.Count, "A4", "")}
			pages := tc.Count
			plan := map[string]any{"version": 4, "output": "packet.pdf", "page_defaults": map[string]any{"size": "A4"}}
			if tc.Numbers {
				plan["page_numbers"] = map[string]any{}
			}
			if tc.Sections {
				items = []any{map[string]any{"contents": map[string]any{}}}
				for n := range tc.Count {
					section := corpusSection(fmt.Sprintf("Section%04d Pielikums Ā Ελληνικά", n), corpusGenerated(1, "A4", ""))
					section["start_on"] = "odd"
					section["divider"] = map[string]any{}
					items = append(items, section)
				}
			}
			if tc.Repeated {
				if err := pdffixture.Pages("REPEAT", 12).WriteFile(filepath.Join(dir, "source.pdf")); err != nil {
					t.Fatal(err)
				}
				items = nil
				for range tc.Count {
					items = append(items, corpusSource("source.pdf", [2]int{1, 1}))
				}
				pages = tc.Count
				if len(items) != 500 {
					t.Fatal("repeated-source authority requires 500 occurrences")
				}
			}
			plan["items"] = items
			path := filepath.Join(dir, "plan.json")
			corpusJSON(t, path, plan)
			var r core.Result
			if tc.Sections {
				report := filepath.Join(dir, "report.json")
				cmd := exec.CommandContext(t.Context(), cli, "build", "--plan", path, "--report", report, "--json")
				cmd.Dir = dir
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				err := cmd.Run()
				if writeErr := os.WriteFile(filepath.Join(dir, "stdout.json"), stdout.Bytes(), 0600); writeErr != nil {
					t.Fatal(writeErr)
				}
				if writeErr := os.WriteFile(filepath.Join(dir, "stderr.txt"), stderr.Bytes(), 0600); writeErr != nil {
					t.Fatal(writeErr)
				}
				if err != nil || stderr.Len() != 0 || stdout.Len() >= 10000 {
					t.Fatalf("500-section CLI output: exit=%v bytes=%d stderr=%s", err, stdout.Len(), &stderr)
				}
				var summary core.Result
				if err = json.Unmarshal(stdout.Bytes(), &summary); err != nil || summary.Status != "ok" || summary.Parts != nil || summary.Sections != nil {
					t.Fatal("bounded CLI summary shape", err)
				}
				schemaValid(t, summary)
				data, err := os.ReadFile(report)
				if err != nil {
					t.Fatal(err)
				}
				if err = json.Unmarshal(data, &r); err != nil {
					t.Fatal(err)
				}
			} else {
				r = core.Execute(t.Context(), core.Request{Operation: core.Build, Input: core.PlanFile{Path: path}, WorkingDir: dir, Report: filepath.Join(dir, "report.json")})
			}

			if r.Status != "ok" {
				t.Fatal(r)
			}
			corpusArtifact(t, r)
			if tc.Sections {
				if len(r.Sections) != tc.Count || len(r.Parts) != tc.Count*2+1 {
					t.Fatal("many-section structure", r.Pages)
				}
				pages = 1016
				if *r.Parts[0].Pages != 16 {
					t.Fatal("reviewed contents pagination changed", r.Parts[0].Pages)
				}
				for n, s := range r.Sections {
					if *s.TargetPage != int64(17+2*n) || *s.OutputPages != [2]int64{int64(17 + 2*n), int64(18 + 2*n)} {
						t.Fatal("literal odd section range", s)
					}
				}
				if *r.Sections[0].TargetPage != *r.Parts[0].Pages+1 || *r.Sections[tc.Count-1].TargetPage != 1015 {
					t.Fatal("section endpoints")
				}
			}
			if *r.Pages.Total != int64(pages) {
				t.Fatal(r.Pages)
			}
			t.Run("oracle", func(t *testing.T) {
				tools := oracle.RequireTools(t)
				if err := tools.Check(r.Output.Path); err != nil {
					t.Fatal("actual scale PDF integrity:", err)
				}
				actual, err := exec.CommandContext(t.Context(), tools.QPDF, "--show-npages", r.Output.Path).Output()
				if err != nil || strings.TrimSpace(string(actual)) != strconv.Itoa(pages) {
					t.Fatalf("independent scale count %s %v", actual, err)
				}
				for _, page := range []int{1, pages} {
					raw, err := exec.CommandContext(t.Context(), tools.PDFToText, "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), r.Output.Path, "-").Output()
					if err != nil {
						t.Fatal(err)
					}
					if tc.Numbers && !strings.HasSuffix(strings.TrimSpace(string(raw)), strconv.Itoa(page)) {
						t.Fatalf("physical scale footer %d: %q", page, raw)
					}
					if tc.Repeated && !strings.Contains(string(raw), "REPEAT p1") {
						t.Fatalf("selected repeat endpoint %q", raw)
					}
				}
				if tc.Repeated {
					if len(r.Parts) != 500 {
						t.Fatal("500 compact occurrences missing")
					}
					for n, p := range r.Parts {
						if p.Spans == nil || len(*p.Spans) != 1 || (*p.Spans)[0].Source != [2]int{1, 1} || (*p.Spans)[0].Output != [2]int64{int64(n + 1), int64(n + 1)} {
							t.Fatal("compact occurrence provenance", p)
						}
					}
				}
				if tc.Sections {
					doc, err := oracle.Load(tools, r.Output.Path)
					if err != nil {
						t.Fatal(err)
					}
					targets, err := doc.OutlineTargets()
					if err != nil || len(targets) != tc.Count || targets[0] != int(*r.Parts[0].Pages)+1 || targets[len(targets)-1] != 1015 {
						t.Fatal("independent scale outline endpoints", err)
					}
				}
			})
		})
	}
}
