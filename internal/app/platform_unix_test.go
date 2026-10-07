// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package app_test

import (
	"syscall"
	"testing"

	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

var errNoSpace error = syscall.ENOSPC

func requireDirectoryPermissions(tb testing.TB) { tb.Helper(); permissiontest.RequireEnforcement(tb) }

func makeReadOnly(tb testing.TB, dir string) func() {
	tb.Helper()
	return permissiontest.DenyDirectoryChanges(tb, dir)
}
