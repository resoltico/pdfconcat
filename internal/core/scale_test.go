// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
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
	data, err := json.Marshal(map[string]any{"version": 3, "output": "packet.pdf", "items": items})
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
			r := core.Execute(t.Context(), core.NewRequest(core.Build, core.FromPlan(core.FilePlan(plan)), core.Options{WorkingDir: dir}))
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
	request := core.NewRequest(core.Build, core.FromPlan(core.FilePlan(plan)), core.Options{WorkingDir: dir, Overwrite: true})
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r := core.Execute(b.Context(), request)
		if r.Status != "ok" || !r.Output.Published {
			b.Fatalf("build: %+v", r.Diagnostics)
		}
	}
}
