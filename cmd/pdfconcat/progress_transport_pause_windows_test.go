// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	progressNativeCancelScenario = "native-cancel"
	progressDeadlineScenario     = "deadline"
	progressConsoleScenario      = "PDFCONCAT_CONSOLE_STALL_SCENARIO"
	progressAbandonedText        = "abandoned-record"
	progressSentinelText         = "independent-sentinel"
)

func TestProgressTransportNativeConsolePause(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	for _, scenario := range []string{"unicode", "release", progressNativeCancelScenario, "caller-cancel", progressDeadlineScenario} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// The outer watchdog bounds a failed fixture, not the product's one-second acceptance limit.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			command := exec.CommandContext(
				ctx,
				executable,
				progressWindowsHelperArgs(t, "-test.run=^TestProgressTransportConsolePauseHelper$", progressVerboseHelperArgument)...)

			command.Env = append(os.Environ(), progressConsoleScenario+"="+scenario)
			output, runErr := command.CombinedOutput()
			t.Logf("private console %s: %s", scenario, output)

			if runErr != nil {
				t.Fatalf("private console fixture failed (watchdog=%v): %v", ctx.Err(), runErr)
			}
		})
	}
}

func TestProgressTransportConsolePauseHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressConsoleScenario)
	if scenario == "" {
		return
	}

	if scenario == "unicode" {
		privateProgressConsole(t)
		assertProgressConsoleUnicodeReadback(t)

		return
	}

	fixture := pausedProgressConsole(t)
	if scenario == "release" || scenario == progressNativeCancelScenario {
		assertProgressRawConsoleBoundary(t, fixture, scenario)
		return
	}

	assertProgressHeldConsoleCancellation(t, fixture, scenario)
}

func assertProgressHeldConsoleCancellation(t *testing.T, fixture *progressConsolePause, scenario string) {
	t.Helper()
	transport := progressNativeTransport(t, fixture.output)
	// Release is registered after transport cleanup: assertion failures release before joining.
	t.Cleanup(fixture.release)
	observer := observeProgressNativeThread(t, transport.threadID)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	result := make(chan error, 1)
	started := time.Now()

	go func() { result <- transport.WriteRecord(ctx, []byte(progressAbandonedText)) }()

	assertProgressNativePending(t, observer, result)
	canceled, cancelSchedule := context.WithCancel(t.Context())
	cancelSchedule()
	// The real worker is held in native I/O, so no receiver can admit this second request.
	attempted, scheduleErr := transport.writeNativeRecord(canceled, []byte(progressTestRecord))
	if attempted || !errors.Is(scheduleErr, context.Canceled) {
		t.Fatalf("canceled scheduling against the held native worker admitted output: %v", scheduleErr)
	}

	wanted := context.DeadlineExceeded
	if scenario == "caller-cancel" {
		wanted = context.Canceled

		cancel()
	}

	err := awaitProgressIOResult(t, result)
	if !errors.Is(err, wanted) || !errors.Is(err, windows.ERROR_OPERATION_ABORTED) || !transport.Interrupted() {
		t.Fatalf("held native console result=%v interrupted=%v", err, transport.Interrupted())
	}

	assertProgressPoisoned(t.Context(), t, transport)
	requireProgressNoError(t, transport.Close())

	if time.Since(started) > time.Second {
		t.Fatal("held native console write/shutdown exceeded one second")
	}

	select {
	case <-transport.stopped:
	default:
		t.Fatal("held native console worker was not joined")
	}

	assertProgressJoinedConsoleCallerAccess(t, fixture)
}

func assertProgressJoinedConsoleCallerAccess(t *testing.T, fixture *progressConsolePause) {
	t.Helper()
	fixture.release()
	_, result, stopped := startProgressConsoleNativeWrite(t, fixture.output, progressSentinelText)
	requireProgressNoError(t, awaitProgressIOResult(t, result))
	<-stopped

	units := readProgressConsoleUnits(t, windows.Handle(fixture.output.Fd()))

	text := windows.UTF16ToString(units)
	text = strings.TrimRight(text, " ")

	prefix, present := strings.CutSuffix(text, progressSentinelText)
	if !present || !strings.HasPrefix(progressAbandonedText, prefix) {
		t.Fatalf("joined console writer left more than its aborted prefix or caller sentinel: %q", text)
	}

	t.Logf("native aborted suffix=%q independent caller sentinel=%q", prefix, progressSentinelText)

	var mode uint32
	requireProgressNoError(t, windows.GetConsoleMode(fixture.handle, &mode))

	codepage, err := windows.GetConsoleOutputCP()
	requireProgressNoError(t, err)

	if windows.Handle(fixture.output.Fd()) != fixture.handle || mode != fixture.mode || codepage != fixture.codepage ||
		progressConsoleHandleFlags(t, fixture.handle) != fixture.flags {
		t.Fatal("aborted progress changed caller console handle, flags, mode or codepage")
	}
}

func awaitProgressIOResult(t *testing.T, result <-chan error) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("native progress write did not complete within one second; endpoint release is cleanup rescue")
		return nil
	}
}
