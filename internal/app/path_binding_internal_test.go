// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestCommandPathsRejectInvalidArgumentsBeforeResourceStages(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"report", "output", "plan", "base"} {
		for _, expression := range []string{"bad\x00path", string([]byte{0xff})} {
			dir := t.TempDir()
			command := &cli.Command{Name: cli.NameCheck}
			switch role {
			case "report":
				command.ReportPath = expression
			case "output":
				command.Output = expression
			case "plan":
				command.PlanPath = expression
				command.PlanSource = cli.PlanFile
			case "base":
				command.BaseDir = expression
			}
			current := &pipeline{command: command, env: Env{WorkingDir: dir}, builder: report.NewBuilder("check")}
			if err := current.resolveCommandPaths(); err == nil || current.status != report.StatusInvalid {
				t.Fatalf("%s argument accepted: %v", role, err)
			}
			if current.workspace != nil || current.captures != nil || current.registry != nil {
				t.Fatal("binding unexpectedly acquired resources")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("rejection changed namespace: %v/%v", entries, err)
			}
			if role == "report" && current.reportPath != "" {
				t.Fatal("invalid report inferred a destination")
			}
		}
	}
}

func TestQueryInvalidPathNeverOpensAnAlternativeFile(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	command := &cli.Command{Name: cli.NameReport, ReportFile: filepath.Join(t.TempDir(), "bad\x00.json"), View: report.ViewParts, HasLimit: true, Limit: 1}
	opens := 0
	code := runQueryWith(t.Context(), command, Env{WorkingDir: t.TempDir(), Stdout: &output}, func(context.Context, string) (*os.File, error) { opens++; return nil, os.ErrNotExist })
	if code != 2 || opens != 0 {
		t.Fatalf("invalid query accessed an inferred path: code=%d opens=%d response=%s", code, opens, &output)
	}
}

func TestInvalidOutputRetainsAuthoredOptionAndArgumentPosition(t *testing.T) {
	t.Parallel()
	for _, option := range []string{"-o", "--output"} {
		for _, joined := range []bool{false, true} {
			expression := "bad\x00output.pdf"
			args := []string{"check", option, expression}
			if joined {
				args = []string{"check", option + "=" + expression}
			}
			current := &pipeline{command: &cli.Command{Name: cli.NameCheck, Output: expression}, env: Env{WorkingDir: t.TempDir(), Arguments: args}, builder: report.NewBuilder("check")}
			if err := current.resolveCommandPaths(); err == nil {
				t.Fatal("invalid output accepted")
			}
			diagnostics := current.builder.Build(current.status).Diagnostics
			if len(diagnostics) != 1 || diagnostics[0].Path != expression || diagnostics[0].Location == nil || diagnostics[0].Location.ArgvIndex == nil || *diagnostics[0].Location.ArgvIndex != 1 || !strings.Contains(diagnostics[0].Message, option+":") {
				t.Fatalf("%s joined=%t lost authored output origin: %+v", option, joined, diagnostics)
			}
		}
	}
}
