// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestModuleDiagnosticSelectionUsesExactModuleBoundaryAcrossTargets(t *testing.T) {
	t.Parallel()

	entry := &repopolicy.Entry{
		ID:     "module-replacement",
		Tool:   repopolicy.ToolLint,
		Effect: repopolicy.EffectExcludeDiagnostic,
		Linter: "gomoddirectives",
		Path:   moduleFileName,
	}
	targets := map[string]map[string]bool{
		"darwin/arm64":         {selectionCommonSource: true},
		selectionWindowsTarget: {selectionCommonSource: true},
	}

	selected, problems := selectDiagnosticEntries([]*repopolicy.Entry{entry}, targets, targets[selectionWindowsTarget])
	if len(problems) != 0 || len(selected) != 1 || selected[0] != entry {
		t.Fatalf("actual module-file diagnostic was not selected: %v %v", selected, problems)
	}

	if stale := repopolicy.StaleDiagnosticEntries(selected, nil, windowsOS); len(stale) != 1 {
		t.Fatal("missing actual module diagnostic did not make its exclusion stale")
	}

	for _, path := range []string{"other.mod", "nested/go.mod", "../go.mod"} {
		entry.Path = path

		_, failures := selectDiagnosticEntries([]*repopolicy.Entry{entry}, targets, targets[selectionWindowsTarget])
		if len(failures) != 1 {
			t.Fatalf("unrelated module path accepted: %s", path)
		}
	}

	entry.Path, entry.Linter = moduleFileName, "gosmopolitan"
	if _, failures := selectDiagnosticEntries([]*repopolicy.Entry{entry}, targets, targets[selectionWindowsTarget]); len(failures) != 1 {
		t.Fatal("Go-file diagnostic adopted module boundary")
	}

	entry.Linter, entry.GOOS = "gomoddirectives", windowsOS
	if _, failures := selectDiagnosticEntries([]*repopolicy.Entry{entry}, targets, targets[selectionWindowsTarget]); len(failures) != 1 {
		t.Fatal("target-independent module diagnostic accepted OS restriction")
	}
}
