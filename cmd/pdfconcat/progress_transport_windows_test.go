// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestProgressTransportNativePipe(t *testing.T) {
	t.Parallel()
	reader, writer := progressPipe(t)
	transport := progressNativeTransport(t, writer)
	record := []byte(progressTestRecord)
	requireProgressNoError(t, transport.WriteRecord(context.Background(), record))
	requireProgressNoError(t, transport.Close())

	got := make([]byte, len(record))
	_, readErr := io.ReadFull(reader, got)
	requireProgressNoError(t, readErr)

	if !bytes.Equal(got, record) {
		t.Fatalf("record: %q", got)
	}

	if err := transport.WriteRecord(context.Background(), record); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed writer: %v", err)
	}

	_, writeErr := writer.Write(record)
	requireProgressNoError(t, writeErr)
}

func TestProgressTransportNativeStoppedPipe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		cancelAfter time.Duration
	}{{name: "deadline", cancelAfter: 0}, {name: "cancellation", cancelAfter: 10 * time.Millisecond}}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			reader, writer := progressPipe(t)
			transport := progressNativeTransport(t, writer)
			// Failure cleanup closes the read endpoint before joining a stalled transport.
			// This is rescue after a failed assertion, never the bounded-write proof.
			closeProgressResource(t, reader)

			record := progressLongRecord(t)

			assertProgressWindowsStoppedWrite(t, transport, record, scenario.cancelAfter)
			assertProgressWindowsRecords(t, reader, writer, record)
		})
	}
}

func TestProgressTransportNativeBrokenPipe(t *testing.T) {
	t.Parallel()
	reader, writer := progressPipe(t)
	transport := progressNativeTransport(t, writer)
	requireProgressNoError(t, reader.Close())

	if err := transport.WriteRecord(context.Background(), []byte(progressEmptyRecord)); err == nil {
		t.Fatal("broken pipe reported success")
	}
}

func assertProgressWindowsStoppedWrite(t *testing.T, transport *progressTransport, record []byte, cancelAfter time.Duration) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	result := make(chan error, 1)
	observer := observeProgressNativeThread(t, transport.threadID)

	go func() { result <- transport.WriteRecord(ctx, record) }()

	// A real undrained pipe write must be pending before caller cancellation.
	assertProgressNativePending(t, observer, result)

	if cancelAfter > 0 {
		time.Sleep(cancelAfter)
		cancel()
	}

	err := awaitProgressIOResult(t, result)

	cancel()

	expected := context.DeadlineExceeded
	if cancelAfter > 0 {
		expected = context.Canceled
	}

	if !errors.Is(err, expected) || !errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
		t.Fatalf("stalled second write: %v", err)
	}

	if !transport.Interrupted() {
		t.Fatal("abandoned native write did not poison sink")
	}

	assertProgressPoisoned(t.Context(), t, transport)
	requireProgressNoError(t, transport.Close())

	if time.Since(start) > time.Second {
		t.Fatal("native write/shutdown exceeded one second")
	}

	select {
	case <-transport.stopped:
	default:
		t.Fatal("owned native writer did not finish")
	}
}

func assertProgressWindowsRecords(t *testing.T, reader, writer *os.File, record []byte) {
	t.Helper()
	requireProgressNoError(t, writer.Close())

	data, readErr := io.ReadAll(reader)
	requireProgressNoError(t, readErr)

	assertProgressTerminalStream(t, data, record)
}
