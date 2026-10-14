// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFuzzStorageRetainsUncertainProducerFilesAndClosesOwnership(t *testing.T) {
	t.Parallel()
	storage, err := newFuzzStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(storage.path) })
	if err := storage.root.WriteFile("tmp/owned", []byte("producer evidence"), fileMode); err != nil {
		t.Fatal(err)
	}
	cause := errors.Join(errors.New("native process metadata unavailable"), fmt.Errorf("cleanup: %w", errFuzzCleanupUncertain))
	result := storage.finish(cause)
	if !errors.Is(result, errFuzzCleanupUncertain) || !strings.Contains(result.Error(), storage.path) {
		t.Fatalf("uncertain cleanup lost its classification or retained path: %v", result)
	}
	if data, err := readInRoot(storage.path, "tmp/owned"); err != nil || string(data) != "producer evidence" {
		t.Fatalf("uncertain producer files removed: %q/%v", data, err)
	}
	if _, err := storage.root.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("retained ownership handle was not closed: %v", err)
	}
}

func TestFuzzStoragePreservesSubstitutedNamespaceAndOriginalFiles(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"directory", "symlink", "missing"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			storage, err := newFuzzStorage(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			moved := storage.path + "-owned"
			t.Cleanup(func() { removeAll(storage.path); removeAll(moved) })
			if err := storage.root.WriteFile("owned", []byte("preserve original"), fileMode); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(storage.path, moved); err != nil {
				t.Fatal(err)
			}
			replacement := t.TempDir()
			switch kind {
			case "directory":
				if err := os.Rename(replacement, storage.path); err != nil {
					t.Fatal(err)
				}
				replacement = storage.path
			case "symlink":
				if err := os.Symlink(replacement, storage.path); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "missing" {
				writeFuzzOwnershipSentinel(t, replacement)
			}
			if err := storage.close(); err == nil {
				t.Fatal("substituted or missing namespace was treated as cleaned")
			}
			if data, err := readInRoot(moved, "owned"); err != nil || string(data) != "preserve original" {
				t.Fatalf("renamed original was deleted: %q/%v", data, err)
			}
			if kind != "missing" {
				if data, err := readInRoot(replacement, "sentinel"); err != nil || string(data) != "outside storage" {
					t.Fatalf("unknown replacement was modified: %q/%v", data, err)
				}
			}
		})
	}
}

func TestFuzzStorageRootedCleanupDoesNotFollowChildSymlink(t *testing.T) {
	t.Parallel()
	storage, err := newFuzzStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	writeFuzzOwnershipSentinel(t, outside)
	if err := storage.root.Symlink(outside, "tmp/outside"); err != nil {
		t.Fatal(err)
	}
	if err := storage.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(storage.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settled storage was not removed: %v", err)
	}
	if data, err := readInRoot(outside, "sentinel"); err != nil || string(data) != "outside storage" {
		t.Fatalf("cleanup followed child symlink: %q/%v", data, err)
	}
}

func TestFuzzStorageDeletionFailureFailsAndPreservesOwnedFiles(t *testing.T) {
	t.Parallel()
	storage, err := newFuzzStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root, err := os.OpenRoot(storage.path)
		if err != nil {
			t.Error(err)
		} else {
			if err := root.Chmod("tmp", 0o700); err != nil {
				t.Error(err)
			}
			if err := root.Close(); err != nil {
				t.Error(err)
			}
		}
		removeAll(storage.path)
	})
	if err := storage.root.WriteFile("tmp/owned", []byte("preserve failure"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := storage.root.Chmod("tmp", 0o500); err != nil {
		t.Fatal(err)
	}
	if err := storage.close(); err == nil || !strings.Contains(err.Error(), storage.path) {
		t.Fatalf("deletion failure was hidden: %v", err)
	}
	if data, err := readInRoot(storage.path, "tmp/owned"); err != nil || string(data) != "preserve failure" {
		t.Fatalf("failed cleanup lost owned data: %q/%v", data, err)
	}
}

func TestFuzzStorageRejectsTemporaryParentInsidePhysicalSourceRoot(t *testing.T) {
	source := t.TempDir()
	inside := filepath.Join(source, "scratch")
	if err := os.Mkdir(inside, dirMode); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{source, inside} {
		t.Setenv("TMPDIR", parent)
		if storage, err := newFuzzStorage(source); storage != nil || !errors.Is(err, errGate) {
			t.Fatalf("recursive source discovery storage accepted: %v/%v", storage, err)
		}
	}
	alias := filepath.Join(t.TempDir(), "temporary-parent")
	if err := os.Symlink(inside, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	if storage, err := newFuzzStorage(source); storage != nil || !errors.Is(err, errGate) {
		t.Fatalf("physical source containment hidden by symlink: %v/%v", storage, err)
	}
	entries, err := os.ReadDir(inside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("rejected parent still received a scratch tree: %v/%v", entries, err)
	}
}

func writeFuzzOwnershipSentinel(t *testing.T, directory string) {
	t.Helper()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	if err := root.WriteFile("sentinel", []byte("outside storage"), fileMode); err != nil {
		t.Fatal(err)
	}
}

func TestFuzzStorageRemovesSettledFailureFiles(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{errors.New("ordinary fuzz assertion failure"), context.Canceled, errGate} {
		storage, err := newFuzzStorage(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if result := storage.finish(cause); !errors.Is(result, cause) {
			t.Fatalf("settled failure verdict was lost: %v", result)
		}
		if _, err := os.Lstat(storage.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("settled failure retained disposable files: %v", err)
		}
	}
}
