// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package app

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/capture"
)

func TestCheckedInputRejectsFIFOAfterEarlierRegularIdentity(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "swapped.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := capture.NewRegistry()
	if _, err := registry.Add(capture.RolePlan, path); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)

	go func() {
		file, err := openInput(t.Context(), path)
		if file != nil {
			err = errors.Join(err, file.Close())
		}

		done <- err
	}()

	select {
	case err := <-done:
		nonregular, ok := errors.AsType[*capture.NotRegularFileError](err)
		if !ok || nonregular.Path != path {
			t.Fatalf("substituted FIFO accepted: %v", err)
		}
	case <-time.After(time.Second):
		release, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			err = release.Close()
		}

		if err != nil {
			t.Errorf("release blocked reader: %v", err)
		}

		t.Fatal("checked input blocked on a FIFO without a writer")
	}
}
