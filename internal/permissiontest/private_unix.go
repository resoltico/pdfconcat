//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package permissiontest

import (
	"os"
	"testing"
)

const (
	privateDirectoryMode os.FileMode = 0o700
	privateFileMode      os.FileMode = 0o600
)

// requirePrivateMode requires owner-only permissions on the actual file or directory.
func requirePrivateMode(tb testing.TB, path string, want os.FileMode) {
	tb.Helper()

	info, err := os.Stat(path)
	if err != nil {
		tb.Fatal(err)
	}

	if info.Mode().Perm() != want {
		tb.Fatalf("private object mode %v, want %v", info.Mode().Perm(), want)
	}
}

// RequirePrivateDirectory requires owner-only access to an actual directory.
func RequirePrivateDirectory(tb testing.TB, path string) {
	tb.Helper()
	requirePrivateMode(tb, path, privateDirectoryMode)
}

// RequirePrivateFile requires owner-only access to an actual file.
func RequirePrivateFile(tb testing.TB, path string) {
	tb.Helper()
	requirePrivateMode(tb, path, privateFileMode)
}
