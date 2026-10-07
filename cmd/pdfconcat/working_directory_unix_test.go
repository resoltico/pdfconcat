// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package main_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

// The shell enters the directory before dropping search permissions, and restores its absolute owned path on exit.
const denyWorkingDirectorySearch = `directory=$1; shift; trap 'chmod 700 "$directory"' EXIT; chmod 000 "$directory" && "$0" "$@"`

func TestUnsearchableWorkingDirectoryReportsFailureAndPathFreeVersionStillWorks(t *testing.T) {
	t.Parallel()
	requireUnprivilegedUser(t)

	shell, lookupErr := exec.LookPath("sh")
	if lookupErr != nil {
		t.Fatalf("required POSIX shell prerequisite unavailable: %v", lookupErr)
	}

	dir := tempDir(t)

	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatal(statErr)
	}

	t.Cleanup(func() {
		if err := os.Chmod(dir, info.Mode().Perm()); err != nil {
			t.Error(err)
		}
	})
	outcome := runWithoutDirectorySearch(t, shell, dir, commandCheck, fileA)
	requireExit(t, outcome, 1)
	parsed := summaryOf(t, outcome)
	requireCode(t, &parsed, "working_directory_unavailable")
	requireAbsent(t, filepath.Join(dir, fileOut))
	requireExit(t, runWithoutDirectorySearch(t, shell, dir, commandVersion), 0)
}

func runWithoutDirectorySearch(t *testing.T, shell, dir string, args ...string) result {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), commandTimeout)
	defer cancel()

	shellArgs := append([]string{"-c", denyWorkingDirectorySearch, binary(t), dir}, args...)
	command := exectest.Command(ctx, shell, shellArgs...)
	command.Dir = dir

	return finish(t, command)
}
