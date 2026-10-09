// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"context"
	"os"
	"runtime"
	"slices"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	progressConsoleTextBuffer  = 1
	progressConsoleOEMCodepage = 437
	progressConsoleReadUnits   = 64
)

// The caller owns a private test subprocess console. Output codepage is
// console-global; restore it before leaving this scope.
func assertProgressConsoleUnicodeReadback(t *testing.T) {
	t.Helper()

	originalCodepage, err := windows.GetConsoleOutputCP()
	requireProgressNoError(t, err)

	requireProgressNoError(t, windows.SetConsoleOutputCP(progressConsoleOEMCodepage))
	defer func() { requireProgressNoError(t, windows.SetConsoleOutputCP(originalCodepage)) }()

	file := progressPrivateConsoleBuffer(t)
	handle := windows.Handle(file.Fd())

	var originalMode uint32
	requireProgressNoError(t, windows.GetConsoleMode(handle, &originalMode))
	requireProgressNoError(t, windows.SetConsoleCursorPosition(handle, windows.Coord{}))
	transport := progressNativeTransport(t, file)
	// Legacy screen-buffer readback is an exact oracle only for spacing BMP text.
	// Non-BMP/combining output needs a validated VT/display observation separately.
	text := "café Ж"
	requireProgressNoError(t, transport.WriteRecord(context.Background(), []byte(text)))
	wanted := utf16.Encode([]rune(text))

	actual := readProgressConsoleUnits(t, handle)
	if len(actual) < len(wanted) || !slices.Equal(actual[:len(wanted)], wanted) {
		t.Fatalf("real console UTF-16 readback %x, expected prefix %x", actual, wanted)
	}

	var mode uint32
	requireProgressNoError(t, windows.GetConsoleMode(handle, &mode))

	codepage, err := windows.GetConsoleOutputCP()
	requireProgressNoError(t, err)

	if mode != originalMode || codepage != progressConsoleOEMCodepage {
		t.Fatal("native progress changed caller console modes or codepage")
	}
}

func progressPrivateConsoleBuffer(t *testing.T) *os.File {
	t.Helper()

	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("CreateConsoleScreenBuffer")

	handle, _, createErr := procedure.Call(
		uintptr(windows.GENERIC_READ|windows.GENERIC_WRITE),
		uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE), 0, progressConsoleTextBuffer, 0,
	)
	if windows.Handle(handle) == windows.InvalidHandle {
		t.Fatalf("create owned inactive console buffer: %v", createErr)
	}

	file := os.NewFile(handle, "progress-console-buffer")
	closeProgressResource(t, file)

	return file
}

func readProgressConsoleUnits(t *testing.T, handle windows.Handle) []uint16 {
	t.Helper()

	var units [progressConsoleReadUnits]uint16

	var count uint32

	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("ReadConsoleOutputCharacterW")

	var pin runtime.Pinner
	pin.Pin(&units[0])

	pin.Pin(&count)
	defer pin.Unpin()

	read, _, readErr := procedure.Call(
		uintptr(handle), uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)), 0, uintptr(unsafe.Pointer(&count)),
	)
	runtime.KeepAlive(&units)
	runtime.KeepAlive(&count)

	if read == 0 {
		t.Fatalf("read owned native console UTF-16: %v", readErr)
	}

	if uint64(count) > uint64(len(units)) {
		t.Fatal("native console read returned impossible unit count")
	}

	return units[:count]
}
