// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestMutationRejectsStaleRegistryBeforeModuleOrToolLookup(t *testing.T) {
	t.Parallel()

	snapshot := t.TempDir()
	file := "platform_windows.go"

	source := "//go:build windows\n\npackage fixture\nfunc Guard(x int) int {\n\tif x > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
	if err := os.WriteFile(filepath.Join(snapshot, file), []byte(source), fileMode); err != nil {
		t.Fatal(err)
	}

	for _, entry := range []*repopolicy.Entry{
		{ID: "stale-coverage", Tool: repopolicy.ToolCoverage, Path: file, Function: "Guard", Anchor: "return 2"},
		{
			ID: "stale-mutation", Tool: repopolicy.ToolMutation, Path: file,
			Anchor: "if x > 0 {", Operator: "CONDITIONALS_BOUNDARY", Column: new(8),
		},
		{ID: "stale-lint", Tool: repopolicy.ToolLint, Path: "missing.go"},
	} {
		registry := &repopolicy.Registry{Exceptions: []*repopolicy.Entry{entry}}

		_, err := mutateSnapshot(t.Context(), &mutationOptions{root: snapshot, snapshot: snapshot}, registry)
		if err == nil || !strings.Contains(err.Error(), "clean mutation snapshot registry") || !strings.Contains(err.Error(), entry.ID) {
			t.Fatalf("stale policy reached module/tool setup: %v", err)
		}
	}

	valid := &repopolicy.Entry{
		ID: "valid", Tool: repopolicy.ToolMutation, Path: file,
		Anchor: "if x > 0 {", Operator: "CONDITIONALS_BOUNDARY", Column: new(7),
	}
	if issues := repopolicy.RepositoryIssues([]*repopolicy.Entry{valid}, fileReader(snapshot)); len(issues) != 0 {
		t.Fatalf("valid platform-tagged source rejected: %v", issues)
	}
}
