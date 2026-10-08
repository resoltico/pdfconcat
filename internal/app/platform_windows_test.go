// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package app_test

import (
	"testing"

	"golang.org/x/sys/windows"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

var errNoSpace error = windows.ERROR_DISK_FULL

func requireDirectoryPermissions(tb testing.TB) { tb.Helper(); permissiontest.RequireEnforcement(tb) }

func makeReadOnly(tb testing.TB, dir string) func() {
	tb.Helper()
	return permissiontest.DenyDirectoryChanges(tb, dir)
}
