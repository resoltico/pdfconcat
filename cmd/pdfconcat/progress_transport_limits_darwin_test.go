// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type progressDescriptorLimitCase struct {
	name        string
	message     string
	terminal    bool
	closeStdout bool
	freeSlot    bool
}

const (
	progressDescriptorLimitScenario = "PDFCONCAT_NATIVE_DESCRIPTOR_LIMIT"
	progressDescriptorLimitSelector = "-test.run=^TestProgressNativeDescriptorLimitHelper$"
	progressDescriptorLimitReceipt  = "native descriptor-limit complete: "
)

func progressDescriptorLimitCases() []progressDescriptorLimitCase {
	return []progressDescriptorLimitCase{
		{name: "pipe-guard-open", freeSlot: true, message: "reserve progress cancellation sink"},
		{name: "pipe-guard-relocation", freeSlot: true, closeStdout: true, message: "protect owned progress descriptor"},
		{name: "terminal-open", terminal: true, message: "open owned progress terminal"},
		{name: "terminal-relocation", terminal: true, closeStdout: true, message: "protect owned progress descriptor"},
	}
}

func TestProgressNativeDescriptorLimits(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	for _, scenario := range progressDescriptorLimitCases() {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			command := exec.CommandContext(ctx, executable, progressHelperArgs(progressDescriptorLimitSelector, "-test.v")...)

			command.Env = append(os.Environ(), progressDescriptorLimitScenario+"="+scenario.name)
			output, runErr := command.CombinedOutput()
			t.Logf("private native descriptor limit %s: %s", scenario.name, output)

			if ctx.Err() != nil || runErr != nil || !strings.Contains(string(output), progressDescriptorLimitReceipt+scenario.name) ||
				!strings.Contains(string(output), "--- PASS: TestProgressNativeDescriptorLimitHelper") {
				t.Fatalf("descriptor-limit scenario not proved (watchdog=%v): %v", ctx.Err(), runErr)
			}
		})
	}
}

func TestProgressNativeDescriptorLimitHelper(t *testing.T) {
	t.Parallel()

	name := os.Getenv(progressDescriptorLimitScenario)
	if name == "" {
		return
	}

	for _, scenario := range progressDescriptorLimitCases() {
		if scenario.name != name {
			continue
		}

		assertProgressDescriptorLimit(t, scenario)

		return
	}

	t.Fatal("unknown native descriptor-limit scenario")
}

func assertProgressDescriptorLimit(t *testing.T, scenario progressDescriptorLimitCase) {
	t.Helper()
	// os.Pipe initializes the actual runtime poller before resource exhaustion.
	_, source := progressPipe(t)
	if scenario.terminal {
		source = progressNativePTY(t)
	}

	initial, err := nativeProgressDescriptor(source)
	requireProgressNoError(t, err)
	flags, err := unix.FcntlInt(initial, unix.F_GETFL, 0)
	requireProgressNoError(t, err)

	var termios *unix.Termios
	if scenario.terminal {
		termios, err = unix.IoctlGetTermios(int(initial), unix.TIOCGETA)
		requireProgressNoError(t, err)
	}

	result := runProgressDescriptorLimit(source, scenario)
	requireProgressNoError(t, result.cleanupErr)

	if !errors.Is(result.operationErr, unix.EMFILE) || !strings.Contains(result.operationErr.Error(), scenario.message) {
		t.Fatalf("wrong native failure stage: %v", result.operationErr)
	}

	if !result.transportNil || !result.slotsPreserved || !result.closedStandardPreserved {
		t.Fatal("constructor retained a transport, leaked an owned slot or occupied closed stdout")
	}

	currentFlags, err := unix.FcntlInt(initial, unix.F_GETFL, 0)
	requireProgressNoError(t, err)

	if currentFlags != flags {
		t.Fatal("constructor failure changed caller file status flags")
	}

	if termios != nil {
		current, termErr := unix.IoctlGetTermios(int(initial), unix.TIOCGETA)
		requireProgressNoError(t, termErr)

		if *current != *termios {
			t.Fatal("constructor failure changed caller terminal mode")
		}
	}

	t.Log(progressDescriptorLimitReceipt + scenario.name)
}
