// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableCoverageRejectsSymbolicLinkInputs(t *testing.T) {
	t.Parallel()

	for _, directory := range []bool{false, true} {
		root, target := t.TempDir(), t.TempDir()
		if !directory {
			target = filepath.Join(target, "regular-file")
			writeForeignGraphFixture(t, target, "not coverage")
		}

		if err := os.Symlink(target, filepath.Join(root, "coverage-link")); err != nil {
			t.Fatal(err)
		}

		if inputs, err := executableCoverageInputs(root); err == nil {
			t.Fatalf("symbolic coverage input accepted: directory=%t inputs=%v", directory, inputs)
		}
	}
}

func TestExecutableCoverageMetadataReconciliationRejectsSymbolicLinks(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"temporary", "canonical"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pair := realCoverageMetadataPair(t)

			path, target := pair.temporary, pair.canonical
			if name == "canonical" {
				path, target = target, path
			}

			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}

			if inputs, err := executableCoverageInputs(pair.directory); err == nil {
				t.Fatalf("symbolic metadata alias reconciled: %s/%v", name, inputs)
			}
		})
	}
}
