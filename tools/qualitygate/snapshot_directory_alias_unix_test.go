// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotCompilerDirectoryAliasesPreservePhysicalContainment(t *testing.T) {
	t.Parallel()

	source, snapshot := t.TempDir(), t.TempDir()
	for _, root := range []string{source, snapshot} {
		if err := os.WriteFile(filepath.Join(root, snapshotDataFile), []byte("same asset"), fileMode); err != nil {
			t.Fatal(err)
		}
	}

	alias := filepath.Join(t.TempDir(), "compiler-directory")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}

	resolved, resolveErr := filepath.EvalSymlinks(source)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}

	pkg := &discoveredPackage{Dir: alias, EmbedFiles: []string{snapshotDataFile}}
	if err := validatePackageEmbeds(source, snapshot, resolved, pkg, map[string]bool{}); err != nil {
		t.Fatalf("same physical compiler directory rejected: %v", err)
	}

	outsideAlias := filepath.Join(t.TempDir(), "outside-directory")
	if err := os.Symlink(snapshot, outsideAlias); err != nil {
		t.Fatal(err)
	}

	pkg.Dir = outsideAlias
	if err := validatePackageEmbeds(source, snapshot, resolved, pkg, map[string]bool{}); err == nil ||
		!strings.Contains(err.Error(), "compiler asset outside snapshot source") {
		t.Fatalf("outside compiler directory alias accepted: %v", err)
	}

	pkg.Dir = filepath.Join(source, "missing-directory")
	if err := validatePackageEmbeds(source, snapshot, resolved, pkg, map[string]bool{}); err == nil ||
		!strings.Contains(err.Error(), "resolve compiler asset package directory") {
		t.Fatalf("unresolved compiler directory accepted: %v", err)
	}
}
