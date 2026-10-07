// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package app_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// namedPipeMode is the permission of a named pipe the tests create.
const namedPipeMode uint32 = 0o600

// makePipe creates a named pipe in dir and releases any reader the test left blocked in an open when it ends.
func makePipe(tb testing.TB, dir, name string) string {
	tb.Helper()

	path := filepath.Join(dir, name)

	err := syscall.Mkfifo(path, namedPipeMode)
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		// Opening a pipe for reading and writing never blocks, and it lets a blocked reader's open return.
		file, openErr := os.OpenFile(filepath.Clean(path), os.O_RDWR, 0)
		if openErr != nil {
			tb.Error(openErr)

			return
		}

		closeErr := file.Close()
		if closeErr != nil {
			tb.Error(closeErr)
		}
	})

	return path
}

func TestOpeningAPlanThatBlocksStopsWithAnInterruption(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	makePipe(t, dir, "pipe.json")

	ctx, cancel := cancelAt(t)
	cancel()

	res := execute(ctx, t, appOf(newFake(t)), dir, commandCheck, planFlag, "pipe.json")
	parsed := res.requireCode(t, 130, outcomeInterrupted)

	// An interruption is not an unreadable plan.
	if parsed.Diagnostics[0].Code != outcomeInterrupted || parsed.Status != outcomeInterrupted {
		t.Errorf(summaryFailureFormat, parsed)
	}
}

func TestAPlanThatCannotBeOpenedIsUnreadable(t *testing.T) {
	t.Parallel()

	requireDirectoryPermissions(t)

	dir := workDir(t)
	writeFile(t, filepath.Join(dir, unreadablePlanPath), planJSON(t, map[string]any{keyItems: []any{sourceA}}))

	err := os.Chmod(filepath.Join(dir, unreadablePlanPath), 0)
	if err != nil {
		t.Fatal(err)
	}

	res := execute(t.Context(), t, appOf(newFake(t)), dir, commandCheck, planFlag, unreadablePlanPath)
	parsed := res.requireCode(t, 1, "plan_unreadable")

	if parsed.Diagnostics[0].Path != filepath.Join(dir, unreadablePlanPath) {
		t.Errorf("diagnostic: %+v", parsed.Diagnostics[0])
	}
}
