// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pipeline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/pipeline"
)

func hasCode(ds []diag.Diagnostic, code diag.Code) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestFileRoleAliasesAndOverwriteRules(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(input, []byte("PDF bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"output", "report"} {
		for _, kind := range []string{"same path", "hard link", "symlink"} {
			t.Run(role+"/"+kind, func(t *testing.T) {
				target := input
				switch kind {
				case "hard link":
					target = filepath.Join(t.TempDir(), "hard.pdf")
					if err := os.Link(input, target); err != nil {
						t.Skipf("hard links unavailable: %v", err)
					}
				case "symlink":
					target = filepath.Join(t.TempDir(), "link.pdf")
					if err := os.Symlink(input, target); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				}
				f := pipeline.Files{Inputs: []string{input}, InputsKnown: true, Overwrite: true}
				if role == "output" {
					f.Output = target
				} else {
					f.Report = target
				}
				ds := f.Check()
				if !hasCode(ds, diag.Code(role+"_is_input")) {
					t.Fatalf("alias not refused: %+v", ds)
				}
				if kind == "symlink" && !hasCode(ds, diag.Code(role+"_not_regular")) {
					t.Fatalf("symlink target not refused: %+v", ds)
				}
			})
		}
	}
	output := filepath.Join(dir, "output.pdf")
	report := filepath.Join(dir, "report.json")
	for _, path := range []string{output, report} {
		if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := pipeline.Files{Output: output, Report: report, InputsKnown: true}
	ds := f.Check()
	if !hasCode(ds, "output_exists") || !hasCode(ds, "report_exists") {
		t.Fatalf("missing overwrite refusals: %+v", ds)
	}
	f.Overwrite = true
	if ds = f.Check(); len(ds) > 0 {
		t.Fatalf("approved overwrite refused: %+v", ds)
	}
	f.InputsKnown = false
	if ds = f.CheckReport(); !hasCode(ds, "report_exists") {
		t.Fatalf("unknown inputs allowed report overwrite: %+v", ds)
	}
	f.Report = f.Output
	if ds = f.CheckReport(); !hasCode(ds, "report_is_output") {
		t.Fatalf("report/output equality: %+v", ds)
	}
	f = pipeline.Files{Output: t.TempDir(), Report: t.TempDir(), Overwrite: true, InputsKnown: true}
	if ds = f.Check(); !hasCode(ds, "output_not_regular") || !hasCode(ds, "report_not_regular") {
		t.Fatalf("nonregular destinations: %+v", ds)
	}
}

func TestReportRoleRecheckFindsInitiallyAbsentParentAlias(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	alias := filepath.Join(dir, "alias")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	output := filepath.Join(real, "packet.pdf")
	report := filepath.Join(alias, "packet.pdf")
	f := pipeline.Files{Output: output, Report: report, Overwrite: true, InputsKnown: true}
	if ds := f.Check(); len(ds) > 0 {
		t.Fatalf("unexpected initial outcome: %+v", ds)
	}
	if err := os.WriteFile(output, []byte("%PDF-published"), 0600); err != nil {
		t.Fatal(err)
	}
	ds := f.CheckReport()
	if !hasCode(ds, "report_is_output") {
		t.Fatalf("late report alias missed: %+v", ds)
	}
	if status, exit := diag.Outcome(ds); status != "invalid" || exit != 2 {
		t.Fatalf("report refusal became an environmental failure: %s %d", status, exit)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "%PDF-published" {
		t.Fatalf("role check changed PDF bytes: %q %v", data, err)
	}
}

func TestCaseAliasOnCaseInsensitiveFilesystem(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "Case.pdf")
	alias := filepath.Join(dir, "case.pdf")
	if err := os.WriteFile(input, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(alias)
	if err != nil || !os.SameFile(first, second) {
		t.Skip("filesystem is case sensitive")
	}
	ds := (pipeline.Files{Output: alias, Inputs: []string{input}, Overwrite: true, InputsKnown: true}).Check()
	if !hasCode(ds, "output_is_input") {
		t.Fatalf("case alias was not refused: %+v", ds)
	}
}
