// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func observeProgressNativeThread(t *testing.T, threadID uint32) windows.Handle {
	t.Helper()

	handle, err := windows.OpenThread(windows.THREAD_QUERY_INFORMATION, false, threadID)
	requireProgressNoError(t, err)
	t.Cleanup(func() { requireProgressNoError(t, windows.CloseHandle(handle)) })

	return handle
}

func assertProgressNativePending(t *testing.T, thread windows.Handle, result <-chan error) {
	t.Helper()

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case err := <-result:
			t.Fatalf("Pause fixture did not hold native console write: %v", err)
		default:
		}

		if progressThreadIOPending(t, thread) {
			select {
			case err := <-result:
				t.Fatalf("native pending snapshot raced completion: %v", err)
			case <-time.After(20 * time.Millisecond):
				if !progressThreadIOPending(t, thread) {
					t.Fatal("console pending I/O did not remain held")
				}

				return
			}
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("Pause fixture never established actual native pending I/O before the transport deadline")
}

func progressThreadIOPending(t *testing.T, thread windows.Handle) bool {
	t.Helper()

	var pending int32

	var pin runtime.Pinner

	pin.Pin(&pending)
	defer pin.Unpin()

	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("GetThreadIOPendingFlag")
	queried, _, queryErr := procedure.Call(uintptr(thread), uintptr(unsafe.Pointer(&pending)))
	runtime.KeepAlive(&pending)

	if queried == 0 {
		t.Fatalf("query actual owned native thread pending I/O: %v", queryErr)
	}

	return pending != 0
}

func startProgressConsoleNativeWrite(t *testing.T, file *os.File, text string) (windows.Handle, <-chan error, <-chan struct{}) {
	t.Helper()

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

		result <- writeProgressConsoleRecord(nil, windows.Handle(file.Fd()), []byte(text))
	}()

	thread := <-ready
	if thread == 0 {
		t.Fatalf("own raw native console writer thread: %v", <-result)
	}

	t.Cleanup(func() {
		<-stopped // The fixture's later cleanup releases first on every assertion failure.
		requireProgressNoError(t, windows.CloseHandle(thread))
	})

	return thread, result, stopped
}

func assertProgressRawConsoleBoundary(t *testing.T, fixture *progressConsolePause, scenario string) {
	t.Helper()
	thread, result, stopped := startProgressConsoleNativeWrite(t, fixture.output, progressAbandonedText)
	t.Cleanup(fixture.release)
	assertProgressNativePending(t, thread, result)

	if scenario == progressNativeCancelScenario {
		assertProgressNativeConsoleAbort(t, thread, result)
		<-stopped
		assertProgressConsoleNoDelayedRecord(t, fixture)

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
