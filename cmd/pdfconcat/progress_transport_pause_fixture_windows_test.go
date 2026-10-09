// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

type (
	progressConsolePause struct {
		input    *os.File
		output   *os.File
		testing  *testing.T
		handle   windows.Handle
		once     sync.Once
		mode     uint32
		codepage uint32
		flags    uint32
	}
	// INPUT_RECORD's KEY_EVENT_RECORD layout is 20 bytes in both native Windows ABIs.
	progressConsoleKeyEvent struct {
		eventType       uint16
		padding         uint16
		keyDown         int32
		repeatCount     uint16
		virtualKeyCode  uint16
		virtualScanCode uint16
		unicodeChar     uint16
		controlKeyState uint32
	}
)

const (
	progressConsoleOutput = "CONOUT$"
	progressFreeConsole   = "FreeConsole"
)

func pausedProgressConsole(t *testing.T) *progressConsolePause {
	t.Helper()
	fixture := privateProgressConsole(t)
	writeProgressConsoleKey(t, fixture.input, 0x13) // VK_PAUSE; host suspension is a source-backed candidate.

	return fixture
}

func privateProgressConsole(t *testing.T) *progressConsolePause {
	t.Helper()

	kernel := windows.NewLazySystemDLL(progressWindowsKernel)

	freed, _, freeErr := kernel.NewProc(progressFreeConsole).Call()
	if freed == 0 && !errors.Is(freeErr, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("detach only test subprocess console: %v", freeErr)
	}

	allocated, _, allocationErr := kernel.NewProc("AllocConsole").Call()
	if allocated == 0 {
		t.Fatalf("allocate private stalled console: %v", allocationErr)
	}

	t.Cleanup(func() {
		closed, _, closeErr := kernel.NewProc(progressFreeConsole).Call()
		if closed == 0 {
			t.Errorf("free private stalled console: %v", closeErr)
		}
	})

	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	requireProgressNoError(t, err)
	closeProgressResource(t, input)

	output, err := os.OpenFile(progressConsoleOutput, os.O_RDWR, 0)
	requireProgressNoError(t, err)
	closeProgressResource(t, output)

	var mode uint32
	requireProgressNoError(t, windows.GetConsoleMode(windows.Handle(input.Fd()), &mode))
	requireProgressNoError(t, windows.SetConsoleMode(windows.Handle(input.Fd()), mode|windows.ENABLE_LINE_INPUT))
	t.Cleanup(func() { requireProgressNoError(t, windows.SetConsoleMode(windows.Handle(input.Fd()), mode)) })
	requireProgressNoError(t, windows.SetConsoleCursorPosition(windows.Handle(output.Fd()), windows.Coord{}))
	handle := windows.Handle(output.Fd())

	var outputMode uint32
	requireProgressNoError(t, windows.GetConsoleMode(handle, &outputMode))

	codepage, err := windows.GetConsoleOutputCP()
	requireProgressNoError(t, err)
	fixture := &progressConsolePause{
		input: input, output: output, testing: t, handle: handle,
		mode: outputMode, codepage: codepage, flags: progressConsoleHandleFlags(t, handle),
	}
	t.Cleanup(fixture.release)

	return fixture
}

func progressConsoleHandleFlags(t *testing.T, handle windows.Handle) uint32 {
	t.Helper()

	var flags uint32

	var pin runtime.Pinner
	pin.Pin(&flags)

	defer pin.Unpin()

	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("GetHandleInformation")
	queried, _, queryErr := procedure.Call(uintptr(handle), uintptr(unsafe.Pointer(&flags)))
	runtime.KeepAlive(&flags)

	if queried == 0 {
		t.Fatalf("query caller console handle flags: %v", queryErr)
	}

	return flags
}

func (fixture *progressConsolePause) release() {
	fixture.once.Do(func() { writeProgressConsoleKey(fixture.testing, fixture.input, 0x1b) }) // VK_ESCAPE
}

func writeProgressConsoleKey(t *testing.T, input *os.File, key uint16) {
	t.Helper()

	record := progressConsoleKeyEvent{eventType: 1, keyDown: 1, repeatCount: 1, virtualKeyCode: key}
	if unsafe.Sizeof(record) != 20 {
		t.Fatal("console key ABI size differs from native INPUT_RECORD")
	}

	var count uint32

	var pin runtime.Pinner
	pin.Pin(&record)

	pin.Pin(&count)
	defer pin.Unpin()

	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("WriteConsoleInputW")
	accepted, _, writeErr := procedure.Call(
		input.Fd(), uintptr(unsafe.Pointer(&record)), 1, uintptr(unsafe.Pointer(&count)),
	)
	runtime.KeepAlive(&record)
	runtime.KeepAlive(&count)

	// Pause and Escape may be consumed as output controls without entering the queue.
	// Pending I/O and actual release/cancellation remain the suspension/completion oracles.
	if accepted == 0 || count > 1 {
		t.Fatalf("inject owned console key: accepted=%d records=%d error=%v", accepted, count, writeErr)
	}

	t.Logf("console output control key=%#x queued_records=%d", key, count)
}
