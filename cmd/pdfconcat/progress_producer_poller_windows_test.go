// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
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

	result := make(chan error, 1)
	done := make(chan struct{})
	readCount := 0

	go func() {
		defer close(done)

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

	assertProgressSDKReadPending(t, result)

	started := time.Now()
	requireProgressNoError(t, source.SetReadDeadline(started.Add(25*time.Millisecond)))
	err := awaitProgressIOResult(t, result)

	<-done

	if !errors.Is(err, os.ErrDeadlineExceeded) || readCount != 0 || time.Since(started) > time.Second {
		t.Fatalf("already-pending Go read ignored its new deadline: count=%d error=%v", readCount, err)
	}

	requireProgressNoError(t, source.SetReadDeadline(time.Time{}))
}

func assertProgressSDKReadPending(t *testing.T, result <-chan error) {
	t.Helper()

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case err := <-result:
			t.Fatalf("SDK read completed before pending-read observation: %v", err)
		default:
		}

		if progressSDKReadParked(t) {
			select {
			case err := <-result:
				t.Fatalf("SDK pending snapshot raced completion: %v", err)
			case <-time.After(20 * time.Millisecond):
				if !progressSDKReadParked(t) {
					t.Fatal("SDK read did not remain parked on its actual IOCP request")
				}

				return
			}
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("SDK overlapped read never entered its actual IOCP wait")
}

func progressSDKReadParked(t *testing.T) bool {
	t.Helper()

	buffer := make([]byte, 1<<20)

	count := runtime.Stack(buffer, true)
	if count == len(buffer) {
		t.Fatal("SDK pending-read stack evidence truncated")
	}

	matched := 0

	for stack := range strings.SplitSeq(string(buffer[:count]), "\n\n") {
		if !strings.Contains(stack, ".assertProgressPendingReadDeadline.func1(") {
			continue
		}

		if strings.Contains(stack, "[IO wait]") && strings.Contains(stack, "internal/poll.(*FD).Read(") &&
			strings.Contains(stack, "internal/poll.(*FD).waitIO(") {
			matched++
		}
	}

	if matched > 1 {
		t.Fatal("SDK pending-read stack matched more than the owned read goroutine")
	}

	return matched == 1
}
