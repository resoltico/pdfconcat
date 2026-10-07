// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package main

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// This test changes the process stream and therefore runs before parallel tests.
func TestProgressUsesWindowsConsoleAndRejectsPipe(t *testing.T) {
	console := progressConsole(t)

	var mode uint32

	if modeErr := windows.GetConsoleMode(windows.Handle(console.Fd()), &mode); modeErr != nil {
		t.Fatalf("real console handle: %v", modeErr)
	}

	previous := os.Stderr

	t.Cleanup(func() { os.Stderr = previous })

	os.Stderr = console

	stream := progressStream()
	if stream != console {
		t.Fatal("interactive console did not receive progress")
	}

	if _, writeErr := stream.Write([]byte("pdfconcat: console progress control\n")); writeErr != nil {
		t.Fatalf("write console progress: %v", writeErr)
	}

	reader, writer, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}

	t.Cleanup(func() {
		if closeErr := writer.Close(); closeErr != nil {
			t.Error(closeErr)
		}

		if closeErr := reader.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	os.Stderr = writer

	if progressStream() != nil {
		t.Fatal("redirected pipe received progress")
	}
}

func progressConsole(t *testing.T) *os.File {
	t.Helper()

	console, openErr := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if openErr != nil {
		kernel := windows.NewLazySystemDLL("kernel32.dll")

		allocated, _, allocationErr := kernel.NewProc("AllocConsole").Call()
		if allocated == 0 {
			t.Fatalf("allocate real console: %v (open: %v)", allocationErr, openErr)
		}

		t.Cleanup(func() {
			freed, _, freeErr := kernel.NewProc("FreeConsole").Call()
			if freed == 0 {
				t.Errorf("free owned console: %v", freeErr)
			}
		})

		console, openErr = os.OpenFile("CONOUT$", os.O_RDWR, 0)
	}

	if openErr != nil {
		t.Fatal(openErr)
	}

	t.Cleanup(func() {
		if closeErr := console.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	return console
}
