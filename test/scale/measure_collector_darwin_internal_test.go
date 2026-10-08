// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

const (
	executableFixtureMode          = 0o700
	descriptorCollectorFixtureName = "collector"
	successfulChildExecutable      = "/usr/bin/true"
)

func TestDescriptorCollectorPreservesConfirmedTerminalFailure(t *testing.T) {
	t.Parallel()

	sampler, prepareErr := prepareProcessSampler()
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}

	t.Cleanup(func() {
		if err := sampler.release(); err != nil {
			t.Error(err)
		}
	})

	child := exec.CommandContext(t.Context(), successfulChildExecutable)
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
	if !reading.terminal || len(reading.failures) != 1 {
		t.Fatalf("confirmedterminal reading: %+v", reading)
	}

	assertTerminalReadingCause(t, reading)
	assertTerminalAttemptCounts(t, reading)
}

func TestDescriptorCollectorFailureWhileProcessLivesRemainsInvalid(t *testing.T) {
	t.Parallel()

	child := exec.CommandContext(t.Context(), sleepingChildExecutable, "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := child.Process.Kill(); err != nil {
			t.Error(err)
		}

		if err := child.Wait(); err == nil {
			t.Error("killedlivechild returned success")
		}
	})

	sampler, err := prepareProcessSampler()
	if err != nil {
		t.Fatal(err)
	}

	if attachErr := sampler.attach(t.Context(), child.Process.Pid); attachErr != nil {
		t.Fatal(attachErr)
	}

	t.Cleanup(func() {
		if closeErr := sampler.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	sampler.collector = "/usr/bin/false"

	reading := sampler.sample(t.Context())
	if reading.terminal || len(reading.failures) != 1 || reading.failures[0].Phase != "live_error" {
		t.Fatalf("livefailurelaundered: %+v", reading)
	}

	var peaks processPeaks
	peaks.record(processSample{descriptors: 10})
	peaks.record(reading)

	_, descriptors := peaks.states(100)
	if descriptors != failedReading || peaks.terminalSamples != 0 {
		t.Fatalf("livefailedread accepted: %+v", peaks)
	}
}

func TestDescriptorCollectorMissingPrerequisiteFailsBeforeProcessLaunch(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if sampler, err := prepareProcessSampler(); err == nil || sampler != nil {
		t.Fatalf("missing lsof prerequisite accepted: %v %+v", err, sampler)
	}
}

func assertTerminalReadingCause(t *testing.T, reading processSample) {
	t.Helper()

	failure := reading.failures[0]
	if failure.Phase != "terminal" || (failure.Probe != "owned_NOTE_EXIT" && failure.Probe != "owned_exit_before_registration: ESRCH") ||
		failure.Stderr != "" ||
		failure.Cause == "" ||
		failure.ReadFinished.Before(failure.ReadStarted) {
		t.Fatalf("terminalcause lost: %+v", failure)
	}
}

func assertTerminalAttemptCounts(t *testing.T, reading processSample) {
	t.Helper()

	var peaks processPeaks
	peaks.record(processSample{descriptors: 10})
	peaks.record(reading)

	rss, descriptors := peaks.states(100)
	if peaks.sampleAttempts != 2 || peaks.descriptorSamples != 1 || peaks.terminalSamples != 1 || rss != validReading ||
		descriptors != validReading {
		t.Fatalf("terminalattempt erased or marked successful: %+v", peaks)
	}
}

func TestCollectorCancellationCannotBecomeTerminalObservation(t *testing.T) {
	t.Parallel()
	sampler := samplerForLiveChild(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assertInvalidCollector(t, sampler.sample(ctx))
}

func TestCollectorPermissionFailureCannotBecomeTerminalObservation(t *testing.T) {
	t.Parallel()
	sampler := samplerForLiveChild(t)

	file := filepath.Join(t.TempDir(), descriptorCollectorFixtureName)
	if err := os.WriteFile(file, []byte("not an executable"), fileMode); err != nil {
		t.Fatal(err)
	}

	sampler.collector = file
	assertInvalidCollector(t, sampler.sample(t.Context()))
}

func TestExitedProcessDoesNotLaunderCollectorStderr(t *testing.T) {
	t.Parallel()
	sampler := samplerForLiveChild(t)

	file := filepath.Join(t.TempDir(), descriptorCollectorFixtureName)

	script := []byte("#!/bin/sh\nkill -TERM \"$2\"\nprintf 'collector failure\\n' >&2\nexit 1\n")
	if writeErr := os.WriteFile(file, script, fileMode); writeErr != nil {
		t.Fatal(writeErr)
	}

	if err := os.Chmod(file, executableFixtureMode); err != nil {
		t.Fatal(err)
	}

	sampler.collector = file
	reading := sampler.sample(t.Context())
	assertInvalidCollector(t, reading)

	if reading.failures[0].Stderr == "" || reading.failures[0].ExitCode != 1 {
		t.Fatalf("native collector error details lost: %+v", reading)
	}
}

func samplerForLiveChild(t *testing.T) *processSampler {
	t.Helper()

	sampler, prepareErr := prepareProcessSampler()
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}

	t.Cleanup(func() {
		if err := sampler.release(); err != nil {
			t.Error(err)
		}
	})

	child := exec.CommandContext(t.Context(), sleepingChildExecutable, "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}

	if err := sampler.attach(t.Context(), child.Process.Pid); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if killErr := child.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Error(killErr)
		}

		reapObservedChild(t, child)
	})

	return sampler
}

func assertInvalidCollector(t *testing.T, reading processSample) {
	t.Helper()

	if reading.terminal || len(reading.failures) != 1 || reading.failures[0].Phase != phaseLiveError {
		t.Fatalf("infrastructure error laundered by process exit: %+v", reading)
	}

	var peaks processPeaks
	peaks.record(processSample{descriptors: 10})
	peaks.record(reading)

	_, state := peaks.states(100)
	if state != failedReading {
		t.Fatalf("broken collector accepted: %+v", peaks)
	}
}

func TestExitedProcessDoesNotLaunderMalformedCollectorOutput(t *testing.T) {
	t.Parallel()
	sampler := samplerForLiveChild(t)

	file := filepath.Join(t.TempDir(), descriptorCollectorFixtureName)

	script := []byte("#!/bin/sh\nkill -TERM \"$2\"\nprintf 'garbage stdout\\n'\nexit 1\n")
	if writeErr := os.WriteFile(file, script, executableFixtureMode); writeErr != nil {
		t.Fatal(writeErr)
	}

	sampler.collector = file
	reading := sampler.sample(t.Context())
	assertInvalidCollector(t, reading)

	if reading.failures[0].Stdout != "garbage stdout\n" || reading.failures[0].ExitCode != 1 {
		t.Fatalf("malformed collector stdout lost: %+v", reading)
	}
}

func TestOwnedExitObserverRecognizesUnreapedChild(t *testing.T) {
	t.Parallel()
	fixture := ownCollectorProcess(t, exec.CommandContext(t.Context(), sleepingChildExecutable, "0.01"))
	reading := awaitTerminalCollector(t, fixture.sampler)
	assertTerminalReadingCause(t, reading)

	if err := syscall.Kill(fixture.command.Process.Pid, 0); err != nil {
		t.Fatalf("child was reaped before observation: %v", err)
	}

	fixture.waitSuccessfully(t)
}

func awaitTerminalCollector(t *testing.T, sampler *processSampler) processSample {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reading := sampler.sample(t.Context())
		if reading.terminal {
			return reading
		}
	}

	t.Fatal("owned exit event did not identify unreaped child's terminal collector reply")

	return processSample{}
}

func TestOwnedExitObserverRegistrationAfterTermination(t *testing.T) {
	t.Parallel()

	fixture := ownCollectorProcess(t, exec.CommandContext(t.Context(), successfulChildExecutable))
	child := fixture.command
	deadline := time.Now().Add(time.Second)

	for {
		output, err := exectest.Command(t.Context(), "/bin/ps", "-p", strconv.Itoa(child.Process.Pid), "-o", "stat=").Output()
		if err != nil {
			t.Fatal(err)
		}

		if strings.HasPrefix(strings.TrimSpace(string(output)), "Z") {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("child did not become an unreaped zombie")
		}
	}

	observer, err := observeProcessExit(t.Context(), child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := observer.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	probe, exited, probeErr := observer.confirm(t.Context(), child.Process.Pid)
	if probeErr != nil || !exited || probe != "owned_exit_before_registration: ESRCH" {
		t.Fatalf("post-exit registration: %s %v", probe, exited)
	}

	fixture.waitSuccessfully(t)
}

func TestOwnedExitEventRejectsUnrelatedOrErroredEvents(t *testing.T) {
	t.Parallel()

	valid := unix.Kevent_t{Ident: 42, Filter: unix.EVFILT_PROC, Fflags: unix.NOTE_EXIT}
	if !ownedExitEvent(valid, 42) {
		t.Fatal("owned exit rejected")
	}

	cases := []unix.Kevent_t{
		{Ident: 43, Filter: unix.EVFILT_PROC, Fflags: unix.NOTE_EXIT},
		{Ident: 42, Filter: unix.EVFILT_READ, Fflags: unix.NOTE_EXIT},
		{Ident: 42, Filter: unix.EVFILT_PROC},
		{Ident: 42, Filter: unix.EVFILT_PROC, Fflags: unix.NOTE_EXIT, Flags: unix.EV_ERROR},
	}
	for _, event := range cases {
		if ownedExitEvent(event, 42) {
			t.Fatalf("unrelated or error event accepted: %+v", event)
		}
	}
}

func TestOwnedExitObserverClosedQueueFailsAndPreservesCleanupError(t *testing.T) {
	observer, err := observeProcessExit(t.Context(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	if closeErr := unix.Close(observer.queue); closeErr != nil {
		t.Fatal(closeErr)
	}

	probe, exited, probeErr := observer.confirm(t.Context(), os.Getpid())
	if exited || probe != "owned_exit_event_error" || !errors.Is(probeErr, unix.EBADF) {
		t.Fatalf("closed queue accepted: %s %v", probe, exited)
	}

	if closeErr := observer.release(); !errors.Is(closeErr, unix.EBADF) {
		t.Fatalf("cleanup failure lost: %v", closeErr)
	}

	if closeErr := observer.release(); closeErr != nil {
		t.Fatalf("release repeated close: %v", closeErr)
	}
}

func reapObservedChild(t *testing.T, child *exec.Cmd) {
	t.Helper()

	waitErr := child.Wait()
	if waitErr == nil {
		return
	}

	exit, exited := errors.AsType[*exec.ExitError](waitErr)
	if !exited || exit.ProcessState == nil {
		t.Error(waitErr)
	}
}

func TestOwnedExitObserverReleasesNonInheritedQueue(t *testing.T) {
	observer, err := observeProcessExit(t.Context(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	queue := observer.queue

	t.Cleanup(func() {
		if closeErr := observer.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	flags, err := unix.FcntlInt(uintptr(queue), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}

	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("exit queue can be inherited by a launched collector")
	}

	if closeErr := observer.release(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if _, statErr := unix.FcntlInt(uintptr(queue), unix.F_GETFD, 0); !errors.Is(statErr, unix.EBADF) {
		t.Fatalf("exit queue leaked after release: %v", statErr)
	}
}

func TestOwnedExitObserverRefusesNonpositivePID(t *testing.T) {
	t.Parallel()

	for _, pid := range []int{0, -1} {
		observer, err := observeProcessExit(t.Context(), pid)
		if !errors.Is(err, errOwnedPID) || observer.attached {
			t.Fatalf("invalid owned PID accepted: %d %+v %v", pid, observer, err)
		}
	}
}

func TestScratchAndCollectorCleanupFailuresBothRemainVisible(t *testing.T) {
	sampler := samplerForCurrentProcess(t)
	if closeErr := unix.Close(sampler.exit.queue); closeErr != nil {
		t.Fatal(closeErr)
	}

	watch := processWatch{sampler: sampler, stop: make(chan struct{})}
	if watch.sampleScratch("invalid\x00scratch") {
		t.Fatal("invalid scratch path accepted")
	}

	peaks, finishErr := watch.finish()

	var pathErr *os.PathError
	if !errors.As(finishErr, &pathErr) || !errors.Is(finishErr, unix.EBADF) || len(peaks.failures) != 2 {
		t.Fatalf("simultaneous errors lost: %+v %v", peaks, finishErr)
	}

	assertReadingFailureEvidence(t, peaks.failures)
}

func samplerForCurrentProcess(t *testing.T) *processSampler {
	t.Helper()

	sampler, err := prepareProcessSampler()
	if err != nil {
		t.Fatal(err)
	}

	if attachErr := sampler.attach(t.Context(), os.Getpid()); attachErr != nil {
		t.Fatal(attachErr)
	}

	t.Cleanup(func() {
		if closeErr := sampler.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	return sampler
}
