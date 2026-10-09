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

	stackBuffer := make([]byte, 1<<20)
	initialDeadline := time.Now().Add(5 * time.Second)
	requireProgressNoError(t, source.SetReadDeadline(initialDeadline))

	result := make(chan error, 1)
	done := make(chan struct{})
	readStarted := make(chan struct{})
	readCount := 0

	go func() {
		defer close(done)

		var (
			buffer [1]byte
			err    error
		)

		close(readStarted)

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

	// Dispatch acknowledgement is setup evidence; two actual SDK wait snapshots remain required.
	select {
	case <-readStarted:
	case <-time.After(time.Until(initialDeadline)):
		t.Fatal("read goroutine did not start before its original native read deadline")
	}

	assertProgressSDKReadPending(t, result, stackBuffer)

	started := time.Now()
	requireProgressNoError(t, source.SetReadDeadline(started.Add(25*time.Millisecond)))
	err := awaitProgressIOResult(t, result)

	<-done

	if !errors.Is(err, os.ErrDeadlineExceeded) || readCount != 0 || time.Since(started) > time.Second {
		t.Fatalf("already-pending Go read ignored its new deadline: count=%d error=%v", readCount, err)
	}

	requireProgressNoError(t, source.SetReadDeadline(time.Time{}))
}

func assertProgressSDKReadPending(t *testing.T, result <-chan error, buffer []byte) {
	t.Helper()

	started := time.Now()
	deadline := started.Add(100 * time.Millisecond)
	queries := 0

	var lastStack string

	var queryStarted, queryFinished time.Time

	query := func() bool {
		queryStarted = time.Now()
		pending, stack := progressSDKReadParked(t, buffer)
		queryFinished = time.Now()
		queries++
		lastStack = stack

		return pending
	}

	for time.Now().Before(deadline) {
		select {
		case err := <-result:
			t.Fatalf("SDK read completed before pending-read observation: %v", err)
		default:
		}

		if query() {
			t.Logf("actual SDK read pending: elapsed=%s queries=%d query_duration=%s",
				time.Since(started), queries, queryFinished.Sub(queryStarted))

			select {
			case err := <-result:
				t.Fatalf("SDK pending snapshot raced completion: %v", err)
			case <-time.After(20 * time.Millisecond):
				if !query() {
					t.Fatalf("SDK read did not remain parked on actual IOCP request: queries=%d result_ready=%t stack=%s",
						queries, len(result) != 0, lastStack)
				}

				return
			}
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("SDK read did not enter IOCP wait: elapsed=%s queries=%d last_query=%s..%s result_ready=%t stack=%s",
		time.Since(started), queries,
		queryStarted.Format(time.RFC3339Nano), queryFinished.Format(time.RFC3339Nano), len(result) != 0, lastStack)
}

func progressSDKReadParked(t *testing.T, buffer []byte) (bool, string) {
	t.Helper()

	count := runtime.Stack(buffer, true)
	if count == len(buffer) {
		t.Fatal("SDK pending-read stack evidence truncated")
	}

	matched := 0
	all := string(buffer[:count])
	owned := ""

	for stack := range strings.SplitSeq(all, "\n\n") {
		if !strings.Contains(stack, ".assertProgressPendingReadDeadline.func1(") {
			continue
		}

		owned = stack

		if strings.Contains(stack, "[IO wait]") && strings.Contains(stack, "internal/poll.(*FD).Read(") &&
			strings.Contains(stack, "internal/poll.(*FD).waitIO(") {
			matched++
		}
	}

	if matched > 1 {
		t.Fatal("SDK pending-read stack matched more than the owned read goroutine")
	}

	if owned == "" {
		owned = all
	}

	return matched == 1, owned
}
