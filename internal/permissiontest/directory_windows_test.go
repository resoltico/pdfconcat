// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package permissiontest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

func TestOwnedDirectoryDACLActuallyRejectsNamespaceChangesAndRestores(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := permissiontest.DenyDirectoryChanges(t, dir)
	assertDeniedDirectoryNamespace(t, dir, existing)

	restore()

	assertRestoredDirectoryNamespace(t, dir, existing)
}

func assertDeniedDirectoryNamespace(t *testing.T, dir, existing string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "blocked"), []byte("x"), 0o600); !os.IsPermission(err) {
		t.Fatalf("FILE_ADD_FILE denial did not reject creation: %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "blocked-directory"), 0o700); !os.IsPermission(err) {
		t.Fatalf("FILE_ADD_SUBDIRECTORY denial did not reject creation: %v", err)
	}

	if err := os.Remove(existing); !os.IsPermission(err) {
		t.Fatalf("directory deletion denial did not preserve existing child: %v", err)
	}
}

func assertRestoredDirectoryNamespace(t *testing.T, dir, existing string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "allowed"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(dir, "allowed-directory"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
	}
}
