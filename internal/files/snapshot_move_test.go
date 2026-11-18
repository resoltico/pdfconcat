// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package files

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Windows directory handles may prevent moving the open directory. This test
// exercises the rename supported by Unix filesystems while a handle is open.
func TestSpillSurvivesScratchDirectoryRename(t *testing.T) {
	parent := t.TempDir()
	scratch, moved := filepath.Join(parent, "scratch"), filepath.Join(parent, "moved")
	if err := os.Mkdir(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", scratch)
	input := filepath.Join(parent, "source.pdf")
	const original = "distinct original bytes"
	if err := os.WriteFile(input, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Acquire(t.Context(), input, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snapshot.Close() })
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one spill: %v, %v", entries, err)
	}
	name := entries[0].Name()
	if err = os.Rename(scratch, moved); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	const replacement = "unrelated file at the former path"
	if err = os.WriteFile(filepath.Join(scratch, name), []byte(replacement), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := snapshot.Open()
	if err != nil {
		t.Error(err)
	} else {
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || string(data) != original {
			t.Errorf("reopened %q, read error %v, close error %v", data, readErr, closeErr)
		}
	}
	if err = snapshot.Close(); err != nil {
		t.Error(err)
	}
	if entries, err = os.ReadDir(moved); err != nil || len(entries) != 0 {
		t.Errorf("moved directory still contains the private spill: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(scratch, name)); err != nil || string(data) != replacement {
		t.Errorf("cleanup touched the replacement directory: %q, %v", data, err)
	}
}
