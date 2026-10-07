// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package capture

import (
	"errors"
	"testing"
)

var errWindowsFIFO = errors.New("POSIX FIFOs are not a Windows filesystem primitive")

func TestWindowsSnapshotRejectsNonDiskDevice(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	if _, err := workspace.Snapshot(t.Context(), "NUL"); !errors.Is(err, errNotDiskFile) {
		t.Fatalf("non-disk device accepted: %v", err)
	}
}

func makeFIFO(string) error { return errWindowsFIFO }
