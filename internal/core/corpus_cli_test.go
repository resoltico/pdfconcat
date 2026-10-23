// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core_test

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/oracle"
)

func corpusCLIContracts(t *testing.T, binary string) {
	for _, args := range [][]string{nil, {"help", "build"}, {"build", "--help"}, {"help", "version"}, {"--version"}, {"version"}, {"version", "--format", "text"}, {"schema", "plan"}, {"schema", "result"}} {
		t.Run("query-"+strings.Join(args, "_"), func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), binary, args...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			raw, err := cmd.Output()
			if err != nil || stderr.Len() != 0 || len(raw) == 0 {
				t.Fatal(err, string(raw), stderr.String())
			}
			if len(args) == 0 {
				for _, word := range []string{"build", "check", "schema", "version"} {
					if !bytes.Contains(raw, []byte(word)) {
						t.Fatalf("root help omits actual command %s: %s", word, raw)
					}
				}
			}
			if len(args) > 0 && args[0] == "version" && len(args) > 2 && args[2] == "text" {
				if !bytes.HasPrefix(raw, []byte("pdfconcat ")) || bytes.Count(raw, []byte("\n")) != 1 || !bytes.Contains(raw, []byte("darwin/")) && !bytes.Contains(raw, []byte("linux/")) && !bytes.Contains(raw, []byte("windows/")) {
					t.Fatalf("useful one-line native version identity absent: %s", raw)
				}
			}
			if len(args) > 0 && (args[0] == "--version" || args[0] == "version" && len(args) == 1) {
				var version core.VersionInfo
				if err = json.Unmarshal(raw, &version); err != nil || version.FormatVersion != 5 || version.Command != "version" || version.Version == "" || version.Go == "" || version.Platform == "" {
					t.Fatalf("installed version facts: %s %v", raw, err)
				}
			}
			if len(args) > 0 && args[0] == "schema" {
				expected, err := core.Schema(core.SchemaKind(args[1]))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(expected)) {
					t.Fatalf("installed %s schema differs from core authority", args[1])
				}
				var schema map[string]any
				if err = json.Unmarshal(raw, &schema); err != nil {
					t.Fatal(err)
				}
				if args[1] == "plan" {
					properties, ok := schema["properties"].(map[string]any)
					if !ok {
						t.Fatal("schema properties missing")
					}
					version, ok := properties["version"].(map[string]any)
					if !ok || version["const"] != float64(3) {
						t.Fatal("plan3 schema fact missing")
					}
				}
			}

		})
	}
	for _, tc := range []struct {
		ID   string
		Args []string
		Code string
	}{{"unknown-command", []string{"merge"}, "usage_unknown_command"}, {"removed-schema", []string{"schema", "response"}, "usage_invalid_value"}, {"duplicate-output", []string{"check", "-o", "a.pdf", "--output", "b.pdf", "source.pdf"}, "usage_conflict"}, {"plan-and-source", []string{"check", "--plan", "plan.json", "source.pdf"}, "usage_conflict"}, {"base-with-named", []string{"check", "--plan", "plan.json", "--base-dir", "."}, "usage_conflict"}, {"missing-value", []string{"check", "--report"}, "usage_missing_value"}, {"missing-input", []string{"build"}, "usage_missing_input"}} {
		t.Run(tc.ID, func(t *testing.T) {
			dir := t.ArtifactDir()
			args := append(append([]string{}, tc.Args...), "--report", filepath.Join(dir, "must-not-exist.json"))
			if tc.ID == "missing-value" {
				args = tc.Args
			}
			cmd := exec.CommandContext(t.Context(), binary, args...)
			cmd.Dir = dir
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			raw, err := cmd.Output()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 || stderr.Len() != 0 {
				t.Fatalf("usage exit %v %s %s", err, raw, &stderr)
			}
			var r core.Result
			if err = json.Unmarshal(raw, &r); err != nil || !code(r, tc.Code) || r.Status != "invalid" || r.Report != nil {
				t.Fatalf("usage before I/O %s %v", raw, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal("usage wrote files", entries, err)
			}
		})
	}
	for _, name := range []string{"--blank", "-"} {
		t.Run("literal-"+name, func(t *testing.T) {
			dir := corpusStage(t, "business-report.pdf")
			corpusCopy(t, filepath.Join(dir, "business-report.pdf"), filepath.Join(dir, name))
			cmd := exec.CommandContext(t.Context(), binary, "check", "--", name)
			cmd.Dir = dir
			raw, err := cmd.Output()
			if err != nil {
				t.Fatal(err, string(raw))
			}
			var r core.Result
			if err = json.Unmarshal(raw, &r); err != nil || *r.Pages.Source != 3 || *r.Pages.Generated != 0 {
				t.Fatalf("literal filename became a flag/stream: %s %v", raw, err)
			}
		})
	}
	t.Run("stdin-check-source-scope", func(t *testing.T) {
		dir := corpusStage(t, "business-report.pdf")
		report := filepath.Join(dir, "report.json")
		cmd := exec.CommandContext(t.Context(), binary, "check", "--plan", "-", "--base-dir", dir, "--report", report)
		cmd.Dir = t.TempDir()
		cmd.Stdin = strings.NewReader(`{"version":3,"output":"must-not-exist.pdf","items":[{"source":"business-report.pdf"}]}`)
		raw, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(raw))
		}
		data, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		var r core.Result
		if err = json.Unmarshal(data, &r); err != nil || r.Parts[0].Path != filepath.Join(dir, "business-report.pdf") || r.Output.Published {
			t.Fatal("stdin lexical base/report facts", r, err)
		}
		if _, err = os.Stat(filepath.Join(dir, "must-not-exist.pdf")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("check emitted PDF")
		}
	})
}

func corpusCLIConcurrentPublication(t *testing.T, binary string) {
	dir := t.ArtifactDir()
	plan := filepath.Join(dir, "plan.json")
	corpusJSON(t, plan, map[string]any{"version": 3, "output": "packet.pdf", "items": []any{corpusGenerated(1000, "A4", "")}})
	commands := []*exec.Cmd{exec.CommandContext(t.Context(), binary, "build", "--plan", plan), exec.CommandContext(t.Context(), binary, "build", "--plan", plan)}
	var outputs, errorsOut [2]bytes.Buffer
	for n, cmd := range commands {
		cmd.Dir = dir
		cmd.Stdout = &outputs[n]
		cmd.Stderr = &errorsOut[n]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	winners := 0
	for n, cmd := range commands {
		err := cmd.Wait()
		var r core.Result
		if decodeErr := json.Unmarshal(outputs[n].Bytes(), &r); decodeErr != nil || errorsOut[n].Len() != 0 {
			t.Fatalf("concurrent result: %s %s %v", &outputs[n], &errorsOut[n], decodeErr)
		}
		if r.Output.Published {
			winners++
			if err != nil || r.Status != "ok" {
				t.Fatal(r, err)
			}
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 && exit.ExitCode() != 2 {
				t.Fatal(r, err)
			}
		}
	}
	if winners != 1 {
		t.Fatal("exactly one real process must own publication", winners)
	}
	t.Run("oracle", func(t *testing.T) {
		tools := oracle.RequireTools(t)
		doc, err := oracle.Load(tools, filepath.Join(dir, "packet.pdf"))
		if err != nil || doc.PageCount() != 1000 {
			t.Fatal(doc, err)
		}
	})
}
