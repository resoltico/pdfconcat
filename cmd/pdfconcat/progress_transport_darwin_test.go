// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProgressPipeCancellationReservesDelayedWriterDescriptor(t *testing.T) {
	t.Parallel()
	_, source := progressPipe(t)
	sink, openErr := openProgressDescriptor(source)
	requireProgressNoError(t, openErr)
	t.Cleanup(func() { requireProgressNoError(t, errors.Join(unix.Close(sink.fd), unix.Close(sink.guard))) })
	requireProgressNoError(t, interruptProgressPipe(sink.fd, sink.guard))

	marker := []byte("unrelated file stays unchanged")
	unrelated, createErr := os.CreateTemp(t.TempDir(), "unrelated")
	requireProgressNoError(t, createErr)
	closeProgressResource(t, unrelated)
	_, markerErr := unrelated.Write(marker)
	requireProgressNoError(t, markerErr)
	// Simulate a worker delayed before entering the native syscall. The FD slot
	// must still be reserved; no late write can hit newly opened unrelated data.
	_, lateErr := unix.Write(sink.fd, []byte("nondelivered progress record\n"))
	_, seekErr := unrelated.Seek(0, io.SeekStart)
	requireProgressNoError(t, seekErr)

	data, readErr := io.ReadAll(unrelated)
	requireProgressNoError(t, readErr)

	if !bytes.Equal(data, marker) {
		t.Fatalf("late progress write changed unrelated file: %q", data)
	}

	if !errors.Is(lateErr, unix.EBADF) {
		t.Fatalf("late native write: %v", lateErr)
	}

	flags, flagErr := unix.FcntlInt(uintptr(sink.fd), unix.F_GETFD, 0)
	requireProgressNoError(t, flagErr)

	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("cancellation guard can leak through exec")
	}
}
