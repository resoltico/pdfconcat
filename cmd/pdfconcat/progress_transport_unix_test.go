// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProgressTransportNativePipe(t *testing.T) {
	t.Parallel()
	reader, writer := progressPipe(t)
	descriptor := int(writer.Fd())
	flags, flagErr := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	requireProgressNoError(t, flagErr)
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

	assertProgressFlags(t, descriptor, flags)

	_, writeErr := writer.Write(record)
	requireProgressNoError(t, writeErr)
}

func TestProgressTransportNativeStoppedPipe(t *testing.T) {
	t.Parallel()

	scenarios := []struct {
		want        error
		name        string
		cancelAfter time.Duration
	}{
		{name: "deadline", cancelAfter: 0, want: context.DeadlineExceeded},
		{name: "cancellation", cancelAfter: 10 * time.Millisecond, want: context.Canceled},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			reader, writer := progressPipe(t)
			record := progressPaddingRecord()
			descriptor, flags := fillProgressPipe(t, writer, record)
			transport := progressNativeTransport(t, writer)

			ctx, cancel := context.WithCancel(context.Background())
			if scenario.cancelAfter > 0 {
				time.AfterFunc(scenario.cancelAfter, cancel)
			}

			start := time.Now()
			err := transport.WriteRecord(ctx, record)

			cancel()

			if !errors.Is(err, scenario.want) {
				t.Fatalf("stalled write: %v", err)
			}

			if !transport.Interrupted() {
				t.Fatal("abandoned native write did not poison sink")
			}

			assertProgressPoisoned(t.Context(), t, transport)
			requireProgressNoError(t, transport.Close())

			if time.Since(start) > time.Second {
				t.Fatal("native write/shutdown exceeded one second")
			}

			assertProgressOwnedDescriptorClosed(t, transport, descriptor)
			assertProgressFlags(t, descriptor, flags)
			assertProgressPipeRecords(t, reader, writer, record)
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

func TestProgressTransportPreservesSharedNonblockingFlags(t *testing.T) {
	t.Parallel()
	_, writer := progressPipe(t)
	descriptor := int(writer.Fd())
	requireProgressNoError(t, unix.SetNonblock(descriptor, true))
	alias, dupErr := unix.Dup(descriptor)
	requireProgressNoError(t, dupErr)
	t.Cleanup(func() { requireProgressNoError(t, unix.Close(alias)) })
	transport := progressNativeTransport(t, writer)
	requireProgressNoError(t, transport.Close())

	for _, shared := range []int{descriptor, alias} {
		flags, flagErr := unix.FcntlInt(uintptr(shared), unix.F_GETFL, 0)
		requireProgressNoError(t, flagErr)

		if flags&unix.O_NONBLOCK == 0 {
			t.Fatal("transport changed shared descriptor flags")
		}
	}
}

func TestProgressTransportInputFailures(t *testing.T) {
	t.Parallel()

	if _, err := newProgressTransport(nil); err == nil {
		t.Fatal("nil stderr accepted")
	}

	_, writer := progressPipe(t)
	transport := progressNativeTransport(t, writer)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := transport.WriteRecord(ctx, []byte(progressEmptyRecord)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write: %v", err)
	}

	if transport.Interrupted() {
		t.Fatal("unsent cancellation poisoned healthy sink")
	}

	requireProgressNoError(t, transport.WriteRecord(context.Background(), []byte(progressEmptyRecord)))
}

func fillProgressPipe(t *testing.T, writer *os.File, record []byte) (int, int) {
	t.Helper()

	descriptor := int(writer.Fd())
	flags, flagErr := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	requireProgressNoError(t, flagErr)
	requireProgressNoError(t, unix.SetNonblock(descriptor, true))

	for writes := 0; ; writes++ {
		if writes > 10000 {
			t.Fatal("undrained pipe never filled")
		}

		count, writeErr := unix.Write(descriptor, record)
		if errors.Is(writeErr, unix.EAGAIN) {
			break
		}

		requireProgressNoError(t, writeErr)

		if count != len(record) {
			t.Fatal("fixture write tore a record")
		}
	}

	_, restoreErr := unix.FcntlInt(uintptr(descriptor), unix.F_SETFL, flags)
	requireProgressNoError(t, restoreErr)

	return descriptor, flags
}

func assertProgressFlags(t *testing.T, descriptor, expected int) {
	t.Helper()

	flags, flagErr := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	requireProgressNoError(t, flagErr)

	if flags&unix.O_NONBLOCK != expected&unix.O_NONBLOCK {
		t.Fatal("transport changed caller O_NONBLOCK")
	}
}

func assertProgressOwnedDescriptorClosed(t *testing.T, transport *progressTransport, sourceDescriptor int) {
	t.Helper()

	select {
	case <-transport.stopped:
	default:
		if transport.interruptiblePipe {
			t.Fatal("owned native writer did not finish")
		}
	}

	var source, owned unix.Stat_t
	requireProgressNoError(t, unix.Fstat(sourceDescriptor, &source))

	if err := unix.Fstat(transport.fd, &owned); err == nil && source.Dev == owned.Dev && source.Ino == owned.Ino {
		t.Fatal("owned stderr descriptor remained open")
	}
}

func assertProgressPipeRecords(t *testing.T, reader, writer *os.File, record []byte) {
	t.Helper()
	requireProgressNoError(t, writer.Close())

	data, readErr := io.ReadAll(reader)
	requireProgressNoError(t, readErr)

	if len(data)%len(record) != 0 {
		t.Fatalf("cancelled write tore record: %d/%d", len(data), len(record))
	}

	if !bytes.Equal(data, bytes.Repeat(record, len(data)/len(record))) {
		t.Fatal("pipe records interleaved")
	}
}

func progressPaddingRecord() []byte {
	record := append([]byte("{\"padding\":\""), bytes.Repeat([]byte("x"), 240)...)
	return append(record, []byte("\"}\n")...)
}
