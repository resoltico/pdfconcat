// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package scale

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOwnedProcessSamplerReadsLiveMetricsAndClosesHandle(t *testing.T) {
	t.Parallel()

	sampler, prepareErr := prepareProcessSampler()
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}

	if err := sampler.attach(t.Context(), os.Getpid()); err != nil {
		t.Fatal(err)
	}

	reading := sampler.sample(t.Context())
	if reading.terminal || reading.descriptors <= 0 || reading.rssBytes <= 0 || reading.rssError || len(reading.failures) != 0 {
		t.Fatalf("native ownedlivequery failed: %+v", reading)
	}

	if err := sampler.release(); err != nil {
		t.Fatal(err)
	}

	closed := sampler.sample(t.Context())
	if closed.terminal || len(closed.failures) != 1 || closed.failures[0].NativeCode != uint64(windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("closed handle laundered as termination: %+v", closed)
	}
}

func TestOwnedProcessTerminationEligibilityRejectsInfrastructureErrors(t *testing.T) {
	t.Parallel()

	for _, code := range []syscall.Errno{windows.ERROR_ACCESS_DENIED, windows.ERROR_INVALID_HANDLE, 0} {
		if eligibleTerminatedQueryError(processSample{failures: []ReadingFailure{{NativeCode: uint64(code)}}}) {
			t.Fatalf("native code %d accepted as terminal", code)
		}
	}

	for _, code := range []syscall.Errno{windows.ERROR_INVALID_PARAMETER, windows.ERROR_PARTIAL_COPY} {
		if !eligibleTerminatedQueryError(processSample{failures: []ReadingFailure{{NativeCode: uint64(code)}}}) {
			t.Fatalf("termination-compatible native code %d rejected", code)
		}
	}
}

func TestOwnedProcessHandleConfirmsNativeChildTermination(t *testing.T) {
	t.Parallel()

	sampler, prepareErr := prepareProcessSampler()
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}

	child := exec.CommandContext(t.Context(), "cmd", "/c", "exit 0")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}

	if err := sampler.attach(t.Context(), child.Process.Pid); err != nil {
		t.Fatal(err)
	}

	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}

	reading := sampler.sample(t.Context())
	if !reading.terminal || len(reading.failures) != 1 || reading.failures[0].Probe != "WAIT_OBJECT_0" {
		t.Fatalf("owned signaledprocess notterminal: %+v", reading)
	}

	if err := sampler.release(); err != nil {
		t.Fatal(err)
	}
}
