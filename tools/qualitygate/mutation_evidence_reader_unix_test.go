//go:build !windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestMutationCaptureReaderRejectsSymlinkAndUnwrittenFIFO(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "regular"), []byte("bytes"), fileMode); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("regular", filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}

	if err := unix.Mkfifo(filepath.Join(directory, "fifo"), fileMode); err != nil {
		t.Fatal(err)
	}

	open := mutationCaptureReader(directory)
	for _, name := range []string{"alias", "fifo"} {
		if file, err := open(name); err == nil {
			if closeErr := file.Close(); closeErr != nil {
				t.Error(closeErr)
			}

			t.Errorf("nonregular artifact accepted: %s", name)
		}
	}
}
