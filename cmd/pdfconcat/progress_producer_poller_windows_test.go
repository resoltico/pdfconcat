// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func assertProgressGoPollerProducer(t *testing.T) {
	t.Helper()
	pair := newProgressNamedPair(t, windows.PIPE_TYPE_BYTE, windows.FILE_FLAG_OVERLAPPED)
	source := pair.wrapClient(t)
	assertProgressPendingReadDeadline(t, source)
	assertProgressProducerRefused(t, source)
	assertProgressPendingReadDeadline(t, source)

	_, err := pair.server.WriteString(progressProducerSentinel)
	requireProgressNoError(t, err)

	data := make([]byte, len(progressProducerSentinel))
	_, err = io.ReadFull(source, data)
	requireProgressNoError(t, err)

	if string(data) != progressProducerSentinel {
		t.Fatal("refused Go-poller producer lost ordinary peer delivery")
	}
}

func assertProgressPendingReadDeadline(t *testing.T, source *os.File) {
	t.Helper()
	requireProgressNoError(t, source.SetReadDeadline(time.Now().Add(5*time.Second)))

	thread := make(chan uint32, 1)
	result := make(chan error, 1)
	done := make(chan struct{})
	readCount := 0

	go func() {
		runtime.LockOSThread()

		defer runtime.UnlockOSThread()
		defer close(done)

		thread <- windows.GetCurrentThreadId()

		var (
			buffer [1]byte
			err    error
		)

		readCount, err = source.Read(buffer[:])
		result <- err
	}()
	// Every unwind joins the SDK-owned async buffer before releasing its file.
	defer func() {
		select {
		case <-done:
		default:
			if err := source.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Errorf("pending Go read cleanup close: %v", err)
			}

			<-done
		}
	}()

	observer := observeProgressNativeThread(t, <-thread)
	assertProgressNativePending(t, observer, result)

	started := time.Now()
	requireProgressNoError(t, source.SetReadDeadline(started.Add(25*time.Millisecond)))
	err := awaitProgressIOResult(t, result)

	<-done

	if !errors.Is(err, os.ErrDeadlineExceeded) || readCount != 0 || time.Since(started) > time.Second {
		t.Fatalf("already-pending Go read ignored its new deadline: count=%d error=%v", readCount, err)
	}

	requireProgressNoError(t, source.SetReadDeadline(time.Time{}))
}
