// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package publish

import (
	"fmt"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSyncDirectory(t *testing.T) {
	t.Parallel()

	err := syncDirectory(t.TempDir())
	if err != nil {
		t.Fatalf("syncDirectory() = %v", err)
	}

	err = syncDirectory(filepath.Join(t.TempDir(), missingPath))
	if err == nil {
		t.Fatal("syncDirectory() accepted a missing directory")
	}
}

func TestDirectorySyncUnsupportedOnlyForUnsupportedErrors(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("sync: %w", syscall.EINVAL)
	if !directorySyncUnsupported(wrapped) || !directorySyncUnsupported(syscall.ENOTSUP) {
		t.Fatal("unsupported errors are not recognized")
	}

	if directorySyncUnsupported(syscall.EIO) {
		t.Fatal("an I/O error was classified as unsupported")
	}
}

func TestFileRefusesNamedPipeDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pipe := filepath.Join(dir, "pipe")

	err := syscall.Mkfifo(pipe, modeOwnerOnly)
	if err != nil {
		t.Fatalf("required FIFO capability unavailable: %v", err)
	}

	err = CheckDestination(pipe, true)
	if err == nil {
		t.Fatal("CheckDestination() accepted a named pipe")
	}
}
