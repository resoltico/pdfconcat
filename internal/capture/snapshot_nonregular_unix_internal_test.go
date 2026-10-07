// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotRejectsFIFOPromptly(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	fifo := filepath.Join(t.TempDir(), "pipe.pdf")

	err := makeFIFO(fifo)
	if err != nil {
		t.Fatalf("required FIFO capability unavailable: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		_, snapErr := workspace.Snapshot(context.Background(), fifo)
		done <- snapErr
	}()

	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Snapshot blocked on a FIFO")
	}

	var irregular *NotRegularFileError
	if !errors.As(err, &irregular) || irregular.Kind != "a named pipe" {
		t.Fatalf("Snapshot(fifo) = %v", err)
	}
}

func TestSnapshotRejectsDevices(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)

	for _, device := range []string{"/dev/zero", "/dev/null"} {
		_, statErr := os.Stat(device)
		if statErr != nil {
			t.Fatalf("required Unix device prerequisite unavailable: %v", statErr)
		}

		done := make(chan error, 1)

		go func() {
			_, err := workspace.Snapshot(context.Background(), device)
			done <- err
		}()

		select {
		case err := <-done:
			var irregular *NotRegularFileError
			if !errors.As(err, &irregular) || irregular.Kind != "a device" {
				t.Fatalf("Snapshot(%s) = %v", device, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("Snapshot(%s) did not return", device)
		}
	}
}
