// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
)

func TestCorpusDocumentReservationRefresh(t *testing.T) {
	oldSource, newSource := filepath.Join(t.TempDir(), "old.pdf"), filepath.Join(t.TempDir(), "new.pdf")
	oldFont, newFont := filepath.Join(t.TempDir(), "old.ttf"), filepath.Join(t.TempDir(), "new.ttf")
	recipe := func(source, font string) []byte {
		data, err := json.Marshal(map[string]any{"version": 4, "items": []any{map[string]any{"source": source}, map[string]any{"page": map[string]any{"size": "A4", "text": map[string]any{"font": map[string]any{"file": font}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	origin := filepath.Join(t.TempDir(), "plan.json")
	original := recipe(oldSource, oldFont)
	if err := os.WriteFile(origin, original, 0600); err != nil {
		t.Fatal(err)
	}
	document, ds := core.ReadPlan(t.Context(), core.FilePlan(origin), filepath.Dir(origin))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	edited, ds := document.ReplaceJSON(recipe(newSource, newFont))
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, ds = edited.ReplaceJSON([]byte(`{"version":4,"items":[null]}`)); len(ds) == 0 {
		t.Fatal("invalid replacement accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tc := range []struct {
		path    string
		written bool
	}{{oldSource, true}, {oldFont, true}, {newSource, false}, {newFont, false}, {origin, false}} {
		r := core.Execute(ctx, core.NewRequest(core.Check, core.FromDocument(edited), core.Options{WorkingDir: filepath.Dir(origin), Report: tc.path, Overwrite: true}))
		if r.Status != "interrupted" || r.Report == nil || r.Report.Written != tc.written {
			t.Fatal(r)
		}
		if !tc.written && !code(r, "report_is_input") {
			t.Fatal("current/origin reservation lost", r)
		}
		if tc.path == origin {
			data, err := os.ReadFile(origin)
			if err != nil || !bytes.Equal(data, original) {
				t.Fatal("physical origin overwritten", err)
			}
		} else if !tc.written {
			if _, err := os.Stat(tc.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("reserved input created", err)
			}
		}
	}
	for _, mode := range []string{"bytes", "loader"} {
		label := filepath.Join(t.TempDir(), mode+"-label.json")
		calls := 0
		source := core.BytesPlan(recipe(newSource, newFont), label, filepath.Dir(origin))
		if mode == "loader" {
			source = core.LoadPlan(label, filepath.Dir(origin), func(context.Context, int64) ([]byte, error) { calls++; return recipe(newSource, newFont), nil })
		}
		r := core.Execute(ctx, core.NewRequest(core.Check, core.FromPlan(source), core.Options{WorkingDir: filepath.Dir(origin), Report: label}))
		if r.Status != "interrupted" || !r.Report.Written || code(r, "report_is_input") || calls != 0 {
			t.Fatal("virtual provenance became a physical reservation", r, calls)
		}
	}
	// Failed replacement also leaves the original document's reservation set intact.
	r := core.Execute(ctx, core.NewRequest(core.Check, core.FromDocument(document), core.Options{WorkingDir: filepath.Dir(origin), Report: oldSource, Overwrite: true}))
	if !code(r, "report_is_input") || r.Report.Written {
		t.Fatal(r)
	}
}
func corpusHiddenPriority(t *testing.T, binary string) {
	dir := t.ArtifactDir()
	var items []core.Item
	args := []string{"check"}
	for n := range 21 {
		name := filepath.Join(dir, fmt.Sprintf("ordinary-%02d.pdf", n))
		if err := os.WriteFile(name, []byte("This is not a PDF.\n"), 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, name)
		items = append(items, core.Source(name, fmt.Sprintf("argv:%d", n+1)))
	}
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			report := filepath.Join(dir, "absent-parent", format+".json")
			cmd := exec.CommandContext(t.Context(), binary, append(append([]string{}, args...), "--report", report, "--format", format)...)
			cmd.Dir = dir
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || stderr.Len() != 0 {
				t.Fatalf("late failed-class exit: %v %s %s", err, &stdout, &stderr)
			}
			if format == "json" {
				var r core.Result
				if err = json.Unmarshal(stdout.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				if r.Status != "failed" || len(r.Diagnostics) != 20 || r.DiagnosticsOmitted != 2 || r.Report.Written {
					t.Fatal(r)
				}
				for _, d := range r.Diagnostics {
					if d.Code.String() != "source_invalid" {
						t.Fatal("late failure incorrectly substituted for visible prefix", r)
					}
				}
			} else if !strings.Contains(stdout.String(), "check failed") || !strings.Contains(stdout.String(), "2 more diagnostics") {
				t.Fatal(stdout.String())
			}
			if err = os.WriteFile(filepath.Join(t.ArtifactDir(), "stdout"), stdout.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
	badReport := filepath.Join(dir, "absent-parent", "core.json")
	full := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromItems(items...), core.Options{WorkingDir: dir, Report: badReport}))
	if len(full.Diagnostics) != 22 || full.Diagnostics[21].Code.String() != "report_write_failed" || full.Status != "failed" || core.ExitCode(full) != 1 {
		t.Fatal(full)
	}
	goodReport := filepath.Join(t.TempDir(), "report.json")
	good := core.Execute(t.Context(), core.NewRequest(core.Check, core.FromItems(items...), core.Options{WorkingDir: dir, Report: goodReport}))
	if len(good.Diagnostics) != 21 || good.Status != "invalid" || core.ExitCode(good) != 2 || !good.Report.Written {
		t.Fatal(good)
	}
	data, err := os.ReadFile(goodReport)
	if err != nil {
		t.Fatal(err)
	}
	var saved core.Result
	if err = json.Unmarshal(data, &saved); err != nil || len(saved.Diagnostics) != 21 || saved.DiagnosticsOmitted != 0 {
		t.Fatal("complete report lost diagnostics", err)
	}
	positive := exec.CommandContext(t.Context(), binary, append(append([]string{}, args...), "--report", filepath.Join(t.TempDir(), "cli.json"))...)
	raw, err := positive.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("safe report positive: %v %s", err, raw)
	}
	corpusJSON(t, filepath.Join(dir, "full-failed.json"), full)
	corpusJSON(t, filepath.Join(dir, "full-invalid.json"), good)
}
