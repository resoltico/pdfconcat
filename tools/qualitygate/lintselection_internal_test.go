// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	selectionUnixSource    = "unix_linux_test.go"
	selectionCommonSource  = "selection.go"
	selectionTestSource    = "selection_test.go"
	activeSelectionID      = "active"
	selectionWindowsTarget = "windows/amd64"
)

func TestDiagnosticSelectionIncludesCompilerChosenTestsAndFixtures(t *testing.T) {
	t.Parallel()
	targets := selectionFixture(t)

	for _, target := range archiveTargets() {
		for _, file := range []string{selectionCommonSource, selectionTestSource, "external_test.go", "testdata/program/main.go"} {
			if !targets[target][file] {
				t.Fatalf("%s excludes actual compiled source%s", target, file)
			}
		}

		if targets[target]["never_test.go"] {
			t.Fatal("unsupported tag selected")
		}
	}
}

func TestActiveDiagnosticStalenessStillFailsAndExcludedUnixIsNotAdjudicated(t *testing.T) {
	t.Parallel()
	targets := selectionFixture(t)

	entries := []*repopolicy.Entry{
		{
			ID:      activeSelectionID,
			Tool:    repopolicy.ToolLint,
			Effect:  repopolicy.EffectExcludeDiagnostic,
			Path:    selectionTestSource,
			Linter:  "gosec",
			Message: "exact message",
		},
		{
			ID:      "unix",
			Tool:    repopolicy.ToolLint,
			Effect:  repopolicy.EffectExcludeDiagnostic,
			Path:    selectionUnixSource,
			Linter:  "depguard",
			Message: "exact message",
		},
	}

	selected, problems := selectDiagnosticEntries(entries, targets, selectionWindowsTarget)
	if len(problems) != 0 || len(selected) != 1 || selected[0].ID != activeSelectionID {
		t.Fatalf("native Windows selection: %v/%v", selected, problems)
	}

	if stale := repopolicy.StaleDiagnosticEntries(selected, nil, "windows"); len(stale) != 1 || stale[0].ID != activeSelectionID {
		t.Fatalf("active stale exclusion escaped: %v", stale)
	}
}

func TestNeverSelectedAndContradictoryDiagnosticScopesFail(t *testing.T) {
	t.Parallel()
	targets := selectionFixture(t)
	entries := []*repopolicy.Entry{
		{ID: activeSelectionID, Tool: repopolicy.ToolLint, Effect: repopolicy.EffectExcludeDiagnostic, Path: selectionTestSource},
		{ID: "unix", Tool: repopolicy.ToolLint, Effect: repopolicy.EffectExcludeDiagnostic, Path: selectionUnixSource},
	}

	entries = append(
		entries,
		&repopolicy.Entry{ID: "never", Tool: repopolicy.ToolLint, Effect: repopolicy.EffectExcludeDiagnostic, Path: "never_test.go"},
	)
	if _, problems := selectDiagnosticEntries(entries, targets, selectionWindowsTarget); len(problems) != 1 {
		t.Fatalf("never-selected exclusion escaped: %v", problems)
	}

	entries = entries[:2]

	entries[1].GOOS = "windows"
	if _, problems := selectDiagnosticEntries(entries, targets, selectionWindowsTarget); len(problems) != 1 {
		t.Fatalf("contradictory explicit scope escaped: %v", problems)
	}
}

func selectionFixture(t *testing.T) map[string]map[string]bool {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		moduleFileName:             "module example.org/selection\ngo 1.27.1\n",
		selectionCommonSource:      "package selection\n",
		selectionTestSource:        "package selection\nimport \"testing\"\nfunc TestFixture(t *testing.T){}\n",
		"external_test.go":         "package selection_test\nimport \"testing\"\nfunc TestExternal(t *testing.T){}\n",
		selectionUnixSource:        "//go:build linux\n\npackage selection\n",
		"unix_darwin_test.go":      "//go:build darwin\n\npackage selection\n",
		"never_test.go":            "//go:build never_selected_here\n\npackage selection\n",
		"testdata/program/main.go": "package main\nfunc main(){}\n",
	}
	for name, body := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(target, []byte(body), fileMode); err != nil {
			t.Fatal(err)
		}
	}

	targets, err := diagnosticTargetFiles(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	return targets
}
