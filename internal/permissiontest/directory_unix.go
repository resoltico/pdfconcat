// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package permissiontest

import (
	"os"
	"testing"
)

const directoryReadSearchMode = 0o500

// DenyDirectoryChanges changes this owned directory's mode and returns an exact restore closure.
func DenyDirectoryChanges(tb testing.TB, dir string) func() {
	tb.Helper()

	info, err := os.Stat(dir)
	if err != nil {
		tb.Fatal(err)
	}

	if chmodErr := os.Chmod(dir, directoryReadSearchMode); chmodErr != nil {
		tb.Fatal(chmodErr)
	}

	restore := func() {
		if restoreErr := os.Chmod(dir, info.Mode().Perm()); restoreErr != nil {
			tb.Error(restoreErr)
		}
	}
	tb.Cleanup(restore)

	return restore
}

// RequireEnforcement rejects a privileged runner that would bypass the required negative control.
func RequireEnforcement(tb testing.TB) {
	tb.Helper()

	if os.Geteuid() == 0 {
		tb.Fatal("required non-root Unix permission-test prerequisite unavailable")
	}
}
