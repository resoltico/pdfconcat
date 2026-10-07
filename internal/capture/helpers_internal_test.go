// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testPermissions = 0o600
	// waitLimit bounds how long a test waits for another goroutine before it reports a failure.
	waitLimit = 30 * time.Second
)

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()

	err := os.WriteFile(path, content, testPermissions)
	if err != nil {
		t.Fatal(err)
	}
}

func newTestWorkspace(t *testing.T) *Workspace {
	t.Helper()

	workspace, err := NewWorkspaceBeside(filepath.Join(t.TempDir(), outputPath))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeWorkspace(t, workspace) })

	return workspace
}

func closeWorkspace(t *testing.T, workspace *Workspace) {
	t.Helper()

	err := workspace.Close()
	if err != nil {
		t.Error(err)
	}
}

// openTestRoot opens the directory holding path so a test can read or modify the file by its base
// name without handing a variable path to the operating system.
func openTestRoot(t *testing.T, path string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := root.Close()
		if closeErr != nil {
			t.Error(closeErr)
		}
	})

	return root
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := openTestRoot(t, path).ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}

	return content
}

// appendTestFile appends text to the existing file at path.
func appendTestFile(t *testing.T, path, text string) {
	t.Helper()

	file, err := openTestRoot(t, path).OpenFile(filepath.Base(path), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, writeErr := file.WriteString(text)
	closeErr := file.Close()

	if writeErr != nil || closeErr != nil {
		t.Fatal(errors.Join(writeErr, closeErr))
	}
}

// createSparseFile creates a file of the given size without writing its content.
func createSparseFile(t *testing.T, path string, size int64) {
	t.Helper()

	file, err := openTestRoot(t, path).Create(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}

	truncateErr := file.Truncate(size)
	closeErr := file.Close()

	if truncateErr != nil || closeErr != nil {
		t.Fatal(errors.Join(truncateErr, closeErr))
	}
}

func scratchEntries(t *testing.T, workspace *Workspace) int {
	t.Helper()

	entries, err := os.ReadDir(workspace.Dir())
	if err != nil {
		t.Fatal(err)
	}

	return len(entries)
}

// requireTestFileClosed uses Read's portable closed-file contract, not platform Stat errno.
func requireTestFileClosed(t *testing.T, file *os.File) {
	t.Helper()

	if _, err := file.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("owned file handle remains usable: %v", err)
	}
}
