// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

type progressThreadObserver struct {
	query  *windows.LazyProc
	thread windows.Handle
}

var errProgressRawConsoleIncomplete = errors.New("independent native console write was incomplete")

func prepareProgressPendingQuery(t *testing.T) *windows.LazyProc {
	t.Helper()

	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("GetThreadIOPendingFlag")
	requireProgressNoError(t, procedure.Find())

	return procedure
}

func observeProgressNativeThread(t *testing.T, threadID uint32) *progressThreadObserver {
	t.Helper()

	handle, err := windows.OpenThread(windows.THREAD_QUERY_INFORMATION, false, threadID)
	requireProgressNoError(t, err)
	t.Cleanup(func() { requireProgressNoError(t, windows.CloseHandle(handle)) })

	return &progressThreadObserver{thread: handle, query: prepareProgressPendingQuery(t)}
}

func assertProgressNativePending(t *testing.T, observer *progressThreadObserver, result <-chan error) {
	t.Helper()

	started := time.Now()
	deadline := started.Add(100 * time.Millisecond)
	queries, pendingSamples := 0, 0

	var queryStarted, queryFinished time.Time

	query := func() bool {
		queryStarted = time.Now()
		pending := progressThreadIOPending(t, observer)
		queryFinished = time.Now()
		queries++

		if pending {
			pendingSamples++
		}

		return pending
	}

	for time.Now().Before(deadline) {
		select {
		case err := <-result:
			t.Fatalf("Pause fixture did not hold native console write: %v", err)
		default:
		}

		if query() {
			t.Logf("actual native pending: elapsed=%s queries=%d query_duration=%s",
				time.Since(started), queries, queryFinished.Sub(queryStarted))

			select {
			case err := <-result:
				t.Fatalf("native pending snapshot raced completion: %v", err)
			case <-time.After(20 * time.Millisecond):
				if !query() {
					t.Fatal("console pending I/O did not remain held")
				}

				return
			}
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatalf("native pending not established: elapsed=%s queries=%d pending_samples=%d last_query=%s..%s result_ready=%t",
		time.Since(started), queries, pendingSamples,
		queryStarted.Format(time.RFC3339Nano), queryFinished.Format(time.RFC3339Nano), len(result) != 0)
}

func progressThreadIOPending(t *testing.T, observer *progressThreadObserver) bool {
	t.Helper()

	var pending int32

	var pin runtime.Pinner

	pin.Pin(&pending)
	defer pin.Unpin()

	thread, procedure := observer.thread, observer.query
	queried, _, queryErr := procedure.Call(uintptr(thread), uintptr(unsafe.Pointer(&pending)))
	runtime.KeepAlive(&pending)

	if queried == 0 {
		t.Fatalf("query actual owned native thread pending I/O: %v", queryErr)
	}

	return pending != 0
}

func startProgressConsoleNativeWrite(t *testing.T, file *os.File, text string) (*progressThreadObserver, <-chan error, <-chan struct{}) {
	t.Helper()
	query := prepareProgressPendingQuery(t)

	ready := make(chan windows.Handle, 1)
	result := make(chan error, 1)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)

		runtime.LockOSThread()

		defer runtime.UnlockOSThread()

		thread, err := windows.OpenThread(windows.THREAD_QUERY_INFORMATION|windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
		ready <- thread

		if err != nil {
			result <- err
			return
		}

		// Independent single native call: no transport encoder, retry or cancellation policy.
		units := utf16.Encode([]rune(text))

		if len(units) == 0 || len(units) > 65535 {
			result <- errProgressRawConsoleIncomplete
			return
		}

		result <- writeIndependentProgressConsole(t, windows.Handle(file.Fd()), units)
	}()

	thread := <-ready
	if thread == 0 {
		t.Fatalf("own raw native console writer thread: %v", <-result)
	}

	t.Cleanup(func() {
		<-stopped // The fixture's later cleanup releases first on every assertion failure.
		requireProgressNoError(t, windows.CloseHandle(thread))
	})

	return &progressThreadObserver{thread: thread, query: query}, result, stopped
}

func writeIndependentProgressConsole(t *testing.T, handle windows.Handle, units []uint16) error {
	t.Helper()

	var written uint32

	var pin runtime.Pinner
	pin.Pin(&units[0])

	pin.Pin(&written)
	defer pin.Unpin()

	for len(units) > 0 {
		requested := uint32(len(units))
		written = 0
		err := windows.WriteConsole(handle, &units[0], requested, &written, nil)
		runtime.KeepAlive(units)
		t.Logf("independent WriteConsoleW requested=%d written=%d error=%v", requested, written, err)

		if err != nil {
			return fmt.Errorf("independent WriteConsoleW: %w", err)
		}

		if written == 0 {
			return io.ErrNoProgress
		}

		if written > requested {
			return errProgressRawConsoleIncomplete
		}

		units = units[written:]
	}

	return nil
}

func assertProgressRawConsoleBoundary(t *testing.T, fixture *progressConsolePause, scenario string) {
	t.Helper()
	thread, result, stopped := startProgressConsoleNativeWrite(t, fixture.output, progressAbandonedText)
	t.Cleanup(fixture.release)
	assertProgressNativePending(t, thread, result)

	if scenario == progressNativeCancelScenario {
		assertProgressNativeConsoleAbort(t, thread.thread, result)
		<-stopped
		assertProgressJoinedConsoleCallerAccess(t, fixture)

		return
	}

	fixture.release()
	requireProgressNoError(t, awaitProgressIOResult(t, result))
	<-stopped

	units := readProgressConsoleUnits(t, windows.Handle(fixture.output.Fd()))
	if !strings.HasPrefix(windows.UTF16ToString(units), progressAbandonedText) {
		t.Fatalf("release-only control lost actual native output: %x", units)
	}
}

func assertProgressNativeConsoleAbort(t *testing.T, thread windows.Handle, result <-chan error) {
	t.Helper()

	started := time.Now()
	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("CancelSynchronousIo")
	accepted, _, cancelErr := procedure.Call(uintptr(thread))
	t.Logf("actual native cancellation accepted=%d error=%v", accepted, cancelErr)

	if accepted == 0 {
		t.Fatalf("cancel positively pending native console: %v", cancelErr)
	}

	err := awaitProgressIOResult(t, result)
	elapsed := time.Since(started)
	t.Logf("actual held native console completion=%v elapsed=%v", err, elapsed)

	if !errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
		t.Fatalf("native console cancellation did not abort the pending operation: %v", err)
	}

	if elapsed > time.Second {
		t.Fatal("held native cancellation boundary exceeded one second")
	}
}
