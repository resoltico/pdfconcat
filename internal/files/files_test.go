// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
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
				f := files.Roles{Inputs: []string{input}, InputsKnown: true, Overwrite: true}
				if role == "output" {
					f.Output = target
				} else {
					f.Report = target
				}
				ds := f.Check()
				if kind != "symlink" && !hasCode(ds, roleCode(role, diag.OutputIsInput, diag.ReportIsInput)) {
					t.Fatalf("alias not refused: %+v", ds)
				}
				if kind == "symlink" && !hasCode(ds, roleCode(role, diag.OutputNotRegular, diag.ReportNotRegular)) {
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
	f := files.Roles{Output: output, Report: report, InputsKnown: true}
	ds := f.Check()
	if !hasCode(ds, diag.OutputExists) || !hasCode(ds, diag.ReportExists) {
		t.Fatalf("missing overwrite refusals: %+v", ds)
	}
	f.Overwrite = true
	if ds = f.Check(); len(ds) > 0 {
		t.Fatalf("approved overwrite refused: %+v", ds)
	}
	f.InputsKnown = false
	if ds = f.CheckReport(); !hasCode(ds, diag.ReportExists) {
		t.Fatalf("unknown inputs allowed report overwrite: %+v", ds)
	}
	f.Report = f.Output
	if ds = f.CheckReport(); !hasCode(ds, diag.ReportIsOutput) {
		t.Fatalf("report/output equality: %+v", ds)
	}
	f = files.Roles{Output: t.TempDir(), Report: t.TempDir(), Overwrite: true, InputsKnown: true}
	if ds = f.Check(); !hasCode(ds, diag.OutputNotRegular) || !hasCode(ds, diag.ReportNotRegular) {
		t.Fatalf("nonregular destinations: %+v", ds)
	}
}

func TestReportRoleRecheckFindsNewParentAlias(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "realDir")
	alias := filepath.Join(dir, "alias")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	output := filepath.Join(realDir, "packet.pdf")
	report := filepath.Join(alias, "packet.pdf")
	f := files.Roles{Output: output, Report: report, Overwrite: true, InputsKnown: true}
	if ds := f.Check(); len(ds) > 0 {
		t.Fatalf("unexpected initial outcome: %+v", ds)
	}
	if err := os.WriteFile(output, []byte("%PDF-published"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, alias); err != nil {
		t.Fatal(err)
	}
	ds := f.CheckReport()
	if len(ds) != 1 || !hasCode(ds, diag.ReportIsOutput) {
		t.Fatalf("late report alias missed: %+v", ds)
	}
	if status := diag.Outcome(ds); status != "invalid" {
		t.Fatalf("report refusal became an environmental failure: %s", status)
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
	ds := (files.Roles{Output: alias, Inputs: []string{input}, Overwrite: true, InputsKnown: true}).Check()
	if !hasCode(ds, diag.OutputIsInput) {
		t.Fatalf("case alias was not refused: %+v", ds)
	}
}

func roleCode(role string, output, report diag.Code) diag.Code {
	if role == "report" {
		return report
	}
	return output
}

func TestReportMissingInputSpellingAndExistingDirectoryAlias(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "missing.pdf")
	roles := files.Roles{Report: dir + string(os.PathSeparator) + "." + string(os.PathSeparator) + "missing.pdf", Inputs: []string{input}, InputsKnown: true, Overwrite: true}
	if ds := roles.CheckReport(); !hasCode(ds, diag.ReportIsInput) {
		t.Fatal(ds)
	}
	roles.Report = filepath.Join(dir, "report.json")
	if ds := roles.CheckReport(); len(ds) != 0 {
		t.Fatal("a separate new report was refused", ds)
	}
	existing := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(existing, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	roles.Report = filepath.Join(alias, "source.pdf")
	roles.Inputs = []string{existing}
	if ds := roles.CheckReport(); !hasCode(ds, diag.ReportIsInput) {
		t.Fatal(ds)
	}
	after, err := os.ReadFile(existing)
	if err != nil || string(after) != "preserve" {
		t.Fatalf("input changed: %q %v", after, err)
	}
}

func TestAbsentSiblingNamesUseConservativeIdentity(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	for _, tc := range []struct {
		otherDir, otherLeaf string
		alias               bool
	}{
		{dir, "Résumé.pdf", true},
		{dir, "re\u0301sume\u0301.pdf", true},
		{dir, "résu\u200bmé.pdf", true},
		{dir, "long-resume.pdf", false},
		{dir, "résumé.json", false},
		{other, "résumé.pdf", false},
	} {
		input := filepath.Join(dir, "résumé.pdf")
		report := filepath.Join(tc.otherDir, tc.otherLeaf)
		roles := files.Roles{Report: report, Inputs: []string{input}, InputsKnown: true}
		if ds := roles.CheckReport(); hasCode(ds, diag.ReportIsInput) != tc.alias || !tc.alias && len(ds) != 0 {
			t.Errorf("%q: %+v", report, ds)
		}
	}
}

func TestDanglingInputLinkResolvesRelativeToNativeParent(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "actual")
	nested := filepath.Join(realParent, "nested")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(nested, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	input := filepath.Join(alias, "input.pdf")
	if err := os.Symlink(filepath.Join("..", "future.pdf"), input); err != nil {
		t.Fatal(err)
	}
	roles := files.Roles{Report: filepath.Join(realParent, "future.pdf"), Inputs: []string{input}, InputsKnown: true}
	if ds := roles.CheckReport(); !hasCode(ds, diag.ReportIsInput) {
		t.Fatalf("dangling link target was missed: %+v", ds)
	}
	roles.Report = filepath.Join(root, "future.pdf")
	if ds := roles.CheckReport(); len(ds) != 0 {
		t.Fatalf("lexically simplified link target was treated as an alias: %+v", ds)
	}
}

func TestDestinationParentMustBeDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "regular")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{filepath.Join(root, "absent"), file, filepath.Join(file, "below")} {
		roles := files.Roles{Output: filepath.Join(parent, "output.pdf"), Report: filepath.Join(parent, "report.json"), InputsKnown: true}
		ds := roles.Check()
		if len(ds) != 2 || !hasCode(ds, diag.OutputParentInvalid) || !hasCode(ds, diag.ReportParentInvalid) {
			t.Errorf("parent %q: %+v", parent, ds)
		}
	}
}

func TestExistingCaseDistinctSiblingsUseNativeIdentity(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "CASE.pdf")
	second := filepath.Join(dir, "case.pdf")
	if err := os.WriteFile(first, nil, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(second, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		t.Skip("filesystem folds case")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	roles := files.Roles{Output: first, Report: second, Overwrite: true, InputsKnown: true}
	if ds := roles.Check(); len(ds) != 0 {
		t.Fatalf("distinct existing files were refused: %+v", ds)
	}
}
