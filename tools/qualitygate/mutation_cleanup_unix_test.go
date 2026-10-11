//go:build !windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMutationSnapshotCleanupFailureCannotPass(t *testing.T) {
	t.Parallel()

	for _, prior := range []error{nil, errGate} {
		snapshot := filepath.Join(t.TempDir(), "snapshot")
		if err := os.Mkdir(snapshot, dirMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(snapshot, "owned"), []byte("fixture"), fileMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(snapshot, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(snapshot, dirMode); err != nil {
				t.Error(err)
			}
		})

		result := prior
		cleanupMutationSnapshot(&result, snapshot)
		if result == nil || !errors.Is(result, os.ErrPermission) {
			t.Fatalf("non-writable snapshot cleanup did not fail command: %v", result)
		}
		if prior != nil && !errors.Is(result, prior) {
			t.Fatalf("cleanup replaced campaign failure: %v", result)
		}
	}
}

func TestMutationSnapshotCleanupDoesNotTraverseSubstitutedSymlink(t *testing.T) {
	t.Parallel()

	external := t.TempDir()
	sentinel := filepath.Join(external, "retain")
	if err := os.WriteFile(sentinel, []byte("outside owned snapshot"), fileMode); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot")
	if err := os.Symlink(external, snapshot); err != nil {
		t.Fatal(err)
	}
	var result error
	cleanupMutationSnapshot(&result, snapshot)
	if result != nil {
		t.Fatal(result)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("cleanup reached outside owned snapshot: %v", err)
	}
	if _, err := os.Lstat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("substituted snapshot link remains: %v", err)
	}
}
