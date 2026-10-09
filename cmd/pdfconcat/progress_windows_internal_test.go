// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"context"
	"os"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

// AllocConsole changes process console ownership, so this runs before parallel tests.
func TestProgressUsesWindowsConsoleAndRejectsPipe(t *testing.T) {
	console := progressConsole(t)

	var mode uint32

	if modeErr := windows.GetConsoleMode(windows.Handle(console.Fd()), &mode); modeErr != nil {
		t.Fatalf("real console handle: %v", modeErr)
	}

	owner := &progressOwner{file: console}
	t.Cleanup(owner.release)

	session := owner.newSession(t.Context(), cli.ProgressAuto, "AAAAAAAAAAAAAAAAAAAAAAAAAA")
	if session == nil {
		t.Fatal("interactive console did not receive progress")
	}

	session.Observe(observation.Milestone{Phase: observation.Preparation})
	session.Stop()

	if owner.interrupted() {
		t.Fatal("normal console progress was interrupted")
	}

	if err := owner.WriteRecord(context.Background(), []byte("pdfconcat: console progress control\n")); err != nil {
		t.Fatal(err)
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

	redirected := &progressOwner{file: writer}
	if redirected.newSession(t.Context(), cli.ProgressAuto, "AAAAAAAAAAAAAAAAAAAAAAAAAA") != nil {
		t.Fatal("redirected pipe received automatic progress")
	}

	if redirected.transport != nil {
		t.Fatal("quiet automatic mode created a transport")
	}
}

func progressConsole(t *testing.T) *os.File {
	t.Helper()

	console, openErr := os.OpenFile(progressConsoleOutput, os.O_RDWR, 0)
	if openErr != nil {
		kernel := windows.NewLazySystemDLL(progressWindowsKernel)

		allocated, _, allocationErr := kernel.NewProc("AllocConsole").Call()
		if allocated == 0 {
			t.Fatalf("allocate real console: %v (open: %v)", allocationErr, openErr)
		}

		t.Cleanup(func() {
			freed, _, freeErr := kernel.NewProc(progressFreeConsole).Call()
			if freed == 0 {
				t.Errorf("free owned console: %v", freeErr)
			}
		})

		console, openErr = os.OpenFile(progressConsoleOutput, os.O_RDWR, 0)
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
