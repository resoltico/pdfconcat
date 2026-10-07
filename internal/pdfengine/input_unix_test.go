//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package pdfengine_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

func TestInspectRejectsFIFOWithoutWaitingForAWriter(t *testing.T) {
	t.Parallel()
	engine := newEngine(t)

	path := filepath.Join(t.TempDir(), "source.fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)

	go func() { _, err := engine.Inspect(t.Context(), path); result <- err }()

	select {
	case err := <-result:
		failure := requireFailure(t, err, pdfengine.CodeUnreadable)
		if failure.Path != path {
			t.Fatal("FIFO rejection lost source path")
		}
	case <-time.After(time.Second):
		t.Fatal("PDF inspection blocked waiting for a FIFO writer")
	}
}

func TestCanceledInspectionDoesNotOpenAMissingNamedInput(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	path := filepath.Join(t.TempDir(), "absent.pdf")
	_, err := newEngine(t).Inspect(ctx, path)

	failure := requireFailure(t, err, pdfengine.CodeCanceled)
	if failure.Path != path {
		t.Fatal("cancellation lost input source identity")
	}
}
