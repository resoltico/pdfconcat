//go:build !windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	mutationRegularName = "regular"
	mutationFIFOName    = "fifo"
)

func TestMutationCaptureReaderRejectsSymlinkAndUnwrittenFIFO(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, mutationRegularName), []byte("bytes"), fileMode); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(mutationRegularName, filepath.Join(directory, "alias")); err != nil {
		t.Fatal(err)
	}

	if err := unix.Mkfifo(filepath.Join(directory, mutationFIFOName), fileMode); err != nil {
		t.Fatal(err)
	}

	open := mutationCaptureReader(directory)
	for _, name := range []string{"alias", mutationFIFOName} {
		if file, err := open(name); err == nil {
			if closeErr := file.Close(); closeErr != nil {
				t.Error(closeErr)
			}

			t.Errorf("nonregular artifact accepted: %s", name)
		}
	}
}

func TestSnapshotRejectsLinkedSourcesConfigsAndFIFO(t *testing.T) {
	t.Parallel()

	source, target := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, mutationRegularName), []byte("bytes"), fileMode); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"linked.go", snapshotConfigFile} {
		if err := os.Symlink(mutationRegularName, filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}

	if err := unix.Mkfifo(filepath.Join(source, mutationFIFOName), fileMode); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"linked.go", snapshotConfigFile, mutationFIFOName} {
		if _, err := copyListed(t.Context(), source, target, name+"\x00"); err == nil {
			t.Fatalf("unsafe source/config snapshot input accepted: %s", name)
		}
	}
}
