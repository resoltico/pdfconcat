//go:build !windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMutationReportPathPreservesNativeParentTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logical := filepath.Join(root, "logical")
	physical := filepath.Join(root, "physical")
	for _, directory := range []string{logical, filepath.Join(physical, "subdir")} {
		if err := os.MkdirAll(directory, dirMode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(physical, "subdir"), filepath.Join(logical, "link")); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{logical, physical} {
		if err := os.WriteFile(filepath.Join(directory, "report.json"), []byte(directory), fileMode); err != nil {
			t.Fatal(err)
		}
	}

	caller, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(caller, logical)
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{logical, relative} {
		expression := base + "/link/../report.json"
		native, err := os.ReadFile(expression)
		if err != nil {
			t.Fatal(err)
		}
		if string(native) != physical {
			t.Fatalf("fixture native traversal selected %q, want physical destination %q", native, physical)
		}
		options, err := parseMutationFlags([]string{"-report", expression})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := os.ReadFile(options.report)
		if err != nil {
			t.Fatal(err)
		}
		if string(selected) != string(native) {
			t.Fatalf("normalized evidence destination %q selected %q; native expression selects %q", options.report, selected, native)
		}
	}
}
