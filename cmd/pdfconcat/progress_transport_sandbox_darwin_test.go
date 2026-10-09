// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin && cgo && progress_native

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

	"github.com/resoltico/pdfconcat/internal/progressnativefixture"
)

const (
	progressSandboxScenario = "PDFCONCAT_NATIVE_TERMINAL_PATH_POLICY"
	progressSandboxSelector = "-test.run=^TestProgressNativeSandboxHelper$"
	progressSandboxReceipt  = "native terminal path policy complete"
)

func TestProgressNativeTerminalPathPolicyFailure(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs(progressSandboxSelector, progressNativeVerbose)...)

	command.Env = append(os.Environ(), progressSandboxScenario+"=1")
	assertProgressNativeChild(ctx, t, command, progressSandboxReceipt, "TestProgressNativeSandboxHelper")
}

func TestProgressNativeSandboxHelper(t *testing.T) {
	t.Parallel()

	if os.Getenv(progressSandboxScenario) == "" {
		return
	}

	// The irreversible sandbox policy belongs only to this actual private process.
	_, _ = progressPipe(t)
	source := progressNativePTY(t)
	initial := progressNativeTransport(t, source)
	requireProgressNoError(t, initial.Close())

	descriptor, err := nativeProgressDescriptor(source)
	requireProgressNoError(t, err)
	baseline, err := progressOpenDescriptorSlots()
	requireProgressNoError(t, err)

	flags := progressDeliveryFlags(t, descriptor)
	termios := progressPollTermios(t, int(descriptor), progressPollTerminal)
	requireProgressNoError(t, progressnativefixture.DenyTerminalPath())

	transport, pathErr := newProgressTransport(source)

	if transport != nil || !errors.Is(pathErr, unix.EPERM) || !strings.Contains(pathErr.Error(), "inspect progress terminal path") {
		t.Fatalf("native F_GETPATH policy failure not returned before admission: transport=%v error=%v", transport, pathErr)
	}

	assertProgressPollOwnership(t, descriptor, baseline, flags, termios)
	t.Log(progressSandboxReceipt)
}
