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

func TestMutationStorageCleanupFailureCannotPass(t *testing.T) {
	t.Parallel()
	for _, prior := range []error{nil, errGate} {
		scratch, watch := mutationStorageFixture(t, campaignDiskUsage{4096, 100})
		snapshot := filepath.Join(scratch.path, "snapshot")
		if err := scratch.root.Mkdir("snapshot", dirMode); err != nil {
			t.Fatal(err)
		}
		watch.setSnapshot(snapshot)
		if err := scratch.root.WriteFile("snapshot/owned", []byte("fixture"), fileMode); err != nil {
			t.Fatal(err)
		}
		if err := scratch.root.Chmod("snapshot", 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			root, err := os.OpenRoot(scratch.path)
			if err != nil {
				t.Error(err)
				return
			}
			if err = errors.Join(root.Chmod("snapshot", dirMode), root.Close()); err != nil {
				t.Error(err)
			}
			removeAll(scratch.path)
		})
		result := prior
		finishMutationStorage(&result, scratch, watch, false)
		if result == nil || !errors.Is(result, os.ErrPermission) {
			t.Fatalf("non-writable storage cleanup did not fail: %v", result)
		}
		if prior != nil && !errors.Is(result, prior) {
			t.Fatalf("cleanup replaced campaign failure: %v", result)
		}
	}
}

func TestMutationStoragePreservesSubstitutedNamespace(t *testing.T) {
	t.Parallel()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "retain"), []byte("outside owned storage"), fileMode); err != nil {
		t.Fatal(err)
	}
	scratch, watch := mutationStorageFixture(t, campaignDiskUsage{4096, 100})
	moved := scratch.path + "-original"
	t.Cleanup(func() { removeAll(moved) })
	if err := scratch.root.WriteFile("source", []byte("owned original"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(scratch.path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, scratch.path); err != nil {
		t.Fatal(err)
	}
	var result error
	finishMutationStorage(&result, scratch, watch, false)
	if !errors.Is(result, errMutationCleanupUncertain) {
		t.Fatalf("substitution accepted as settled: %v", result)
	}
	if data, err := readInRoot(external, "retain"); err != nil || string(data) != "outside owned storage" {
		t.Fatalf("outside directory changed: %q/%v", data, err)
	}
	if data, err := readInRoot(moved, "source"); err != nil || string(data) != "owned original" {
		t.Fatalf("original directory changed: %q/%v", data, err)
	}
	if info, err := os.Lstat(scratch.path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("unknown replacement removed: %v/%v", info, err)
	}
}
