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

func TestPlanBytesLabelIsNotAPhysicalInput(t *testing.T) {
	dir := t.TempDir()
	label := filepath.Join(dir, "virtual.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := core.Execute(ctx, core.Request{Operation: core.Check, Input: core.PlanBytes{Data: []byte(`{"version":4,"items":[{"source":"missing.pdf"}]}`), Label: label}, WorkingDir: dir, Report: label})
	if r.Status != "interrupted" || !r.Report.Written || code(r, "report_is_input") {
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
		items = append(items, core.SourceItem{Path: name, Ref: fmt.Sprintf("argv:%d", n+1)})
	}
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			report := filepath.Join(dir, "absent-parent", format+".json")
			commandArgs := append(append([]string{}, args...), "--report", report)
			if format == "json" {
				commandArgs = append(commandArgs, "--json")
			}
			cmd := exec.CommandContext(t.Context(), binary, commandArgs...)
			cmd.Dir = dir
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || format == "json" && stderr.Len() != 0 {
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
			} else if !strings.Contains(stdout.String(), "check failed") || !strings.Contains(stderr.String(), "2 more diagnostics") {
				t.Fatal(stdout.String())
			}
			if err = os.WriteFile(filepath.Join(t.ArtifactDir(), "stdout"), stdout.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
	badReport := filepath.Join(dir, "absent-parent", "core.json")
	full := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.Items(items), WorkingDir: dir, Report: badReport})
	if len(full.Diagnostics) != 22 || full.Diagnostics[21].Code.String() != "report_write_failed" || full.Status != "failed" || core.ExitCode(full) != 1 {
		t.Fatal(full)
	}
	goodReport := filepath.Join(t.TempDir(), "report.json")
	good := core.Execute(t.Context(), core.Request{Operation: core.Check, Input: core.Items(items), WorkingDir: dir, Report: goodReport})
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
