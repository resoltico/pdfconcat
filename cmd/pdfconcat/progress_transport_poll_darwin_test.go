// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	progressPollLimitScenario = "PDFCONCAT_NATIVE_POLL_LIMIT"
	progressPollLimitReceipt  = "native poll-limit complete: "
	progressPollPipe          = "pipe"
	progressPollTerminal      = "terminal"
)

func TestProgressNativePollResourceFailure(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{progressPollPipe, progressPollTerminal} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			trace, err := runProgressPhaseChild(t, "TestProgressNativePollLimitHelper", []string{progressPollLimitScenario + "=" + scenario}, 3*time.Second, 10*time.Second)
			if err != nil {
				t.Fatalf("native resource lifetime: %v; %s", err, trace)
			}
			t.Log(trace)
		})
	}
}

func TestProgressNativePollLimitHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressPollLimitScenario)
	if scenario == "" {
		return
	}

	if scenario != progressPollPipe && scenario != progressPollTerminal {
		t.Fatal("unknown native poll-limit scenario")
	}

	if unix.Getuid() == 0 {
		t.Fatal("native Darwin poll-limit control requires a nonroot process")
	}

	control := progressPhaseControl(t)
	if _, err := fmt.Fprintln(control, "ready"); err != nil {
		t.Fatal(err)
	}
	assertProgressPollLimit(t, scenario)
	if _, err := fmt.Fprintln(control, "operation"); err != nil {
		t.Fatal(err)
	}
}

func assertProgressPollLimit(t *testing.T, scenario string) {
	t.Helper()

	// Initialize runtime netpoll and timers before lowering the native limit.
	_, source := progressPipe(t)

	time.Sleep(time.Millisecond)

	if scenario == progressPollTerminal {
		source = progressNativePTY(t)
	}

	descriptor, err := nativeProgressDescriptor(source)
	requireProgressNoError(t, err)

	if scenario == progressPollPipe {
		requireProgressNoError(t, unix.SetNonblock(int(descriptor), true))
	}

	baseline, err := progressOpenDescriptorSlots()
	requireProgressNoError(t, err)
	transport := progressNativeTransport(t, source)
	fillProgressNativeSink(t, transport.fd)
	flags := progressDeliveryFlags(t, descriptor)
	termios := progressPollTermios(t, int(descriptor), scenario)
	result := runProgressPollLimit(t.Context(), transport)
	requireProgressNoError(t, result.restoreErr)

	message := "poll progress pipe"
	if scenario == progressPollTerminal {
		message = "poll progress sink"
	}

	if !errors.Is(result.writeErr, unix.EINVAL) || !strings.Contains(result.writeErr.Error(), message) {
		t.Fatalf("native poll EINVAL not established: %v", result.writeErr)
	}

	if !transport.Interrupted() {
		t.Fatal("real native poll failure left telemetry usable")
	}

	assertProgressPoisoned(t.Context(), t, transport)
	requireProgressNoError(t, transport.Close())
	assertProgressPollOwnership(t, descriptor, baseline, flags, termios)
	t.Log(progressPollLimitReceipt + scenario)
}
