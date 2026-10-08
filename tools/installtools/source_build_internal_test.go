// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceDependencyLockRejectsModifiedUpstreamGraphFiles(t *testing.T) {
	t.Parallel()

	for _, name := range []string{sourceModuleFile, "go.sum"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeOriginalSourceDependencyFiles(t, dir)

			locked, lockErr := snapshotSourceDependencies(dir)
			if lockErr != nil {
				t.Fatal(lockErr)
			}

			if err := locked.verify(dir); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(dir, name), []byte("different selected dependencies"), privateMode); err != nil {
				t.Fatal(err)
			}

			if err := locked.verify(dir); !errors.Is(err, errInstall) {
				t.Fatalf("changed upstream graph accepted: %v", err)
			}
		})
	}
}

func TestSourcePatchRejectsChangedDigestBeforeExecutingGit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := applySourcePatch(t.Context(), dir, []byte("tampered source patch"), "wrong"); !errors.Is(err, errInstall) {
		t.Fatalf("tampered patch accepted: %v", err)
	}

	files, readErr := os.ReadDir(dir)
	if readErr != nil || len(files) != 0 {
		t.Fatalf("tampered patch changed source: %v %v", files, readErr)
	}
}

func writeOriginalSourceDependencyFiles(t *testing.T, dir string) {
	t.Helper()

	for _, file := range []string{sourceModuleFile, "go.sum"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("original upstream bytes"), privateMode); err != nil {
			t.Fatal(err)
		}
	}
}
