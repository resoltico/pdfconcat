// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package capture

import (
	"errors"
	"os"
	"testing"
)

var errWindowsFIFO = errors.New("POSIX FIFOs are not a Windows filesystem primitive")

func TestWindowsSnapshotRejectsNonDiskDevice(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	if _, err := workspace.Snapshot(t.Context(), os.DevNull); !errors.Is(err, errNotDiskFile) {
		t.Fatalf("non-disk device accepted: %v", err)
	}
}

func makeFIFO(string) error { return errWindowsFIFO }

func TestWindowsIdentityAndArtifactRejectNonDiskDevice(t *testing.T) {
	t.Parallel()

	if identity, err := IdentityOf(os.DevNull); err == nil || identity != (Identity{}) {
		t.Fatalf("non-filesystem identity accepted: %v %v", identity, err)
	}

	if _, err := NewRegistry().Add(RoleOutput, `\\.\NUL`); err == nil {
		t.Fatal("non-disk artifact target accepted")
	}

	device, openErr := os.Open(os.DevNull)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer func() {
		if closeErr := device.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()

	if identity, err := IdentityOfFile(device); err == nil || identity != (Identity{}) {
		t.Fatalf("non-filesystem open handle identity accepted: %v %v", identity, err)
	}
}
