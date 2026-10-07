// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package permissiontest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

func TestOwnedDirectoryRefusesCreationDeletionAndRestores(t *testing.T) {
	t.Parallel()
	permissiontest.RequireEnforcement(t)
	dir := t.TempDir()

	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := permissiontest.DenyDirectoryChanges(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "blocked"), []byte("x"), 0o600); !os.IsPermission(err) {
		t.Fatalf("creation unexpectedly allowed: %v", err)
	}

	if err := os.Remove(existing); !os.IsPermission(err) {
		t.Fatalf("deletion unexpectedly allowed: %v", err)
	}

	restore()

	if err := os.WriteFile(filepath.Join(dir, "allowed"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
	}
}
