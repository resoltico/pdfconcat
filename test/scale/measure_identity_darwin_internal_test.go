// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build darwin

package scale

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type collectorProcessFixture struct {
	command     *exec.Cmd
	sampler     *processSampler
	waitCalled  bool
	closedQueue bool
}

const blockedCollectorScript = `#!/bin/sh
dir=${0%/*}
printf 'started' > "$dir/started"
while [ ! -e "$dir/released" ]; do /bin/sleep 0.005; done
printf 'p%s\n' "$2"
i=0
while [ "$i" -lt 96 ]; do printf 'f%s\n' "$i"; i=$((i+1)); done
`

func TestSuccessfulCollectorReplyAfterOwnedWaitCannotBecomeDescriptorReading(t *testing.T) {
	t.Parallel()
	fixture := startCollectorProcessFixture(t)

	reading := blockedCollectorReading(t, fixture.sampler, fixture.killAndWait)
	if !reading.terminal || reading.descriptors != -1 || len(reading.failures) != 1 {
		t.Fatalf("foreign numeric-PID reply accepted: %+v", reading)
	}

	failure := reading.failures[0]
	if failure.Stdout == "" || !strings.Contains(failure.Cause, "cannot be attributed") {
		t.Fatalf("discarded reply evidence lost: %+v", failure)
	}

	assertReadingFailureEvidence(t, reading.failures)
	assertUntrustedReplyDoesNotContributePeak(t, reading)
	assertOwnedExitRemainsLatched(t, fixture.sampler)
}

func TestSuccessfulCollectorReplyCannotHideClosedExitQueue(t *testing.T) {
	fixture := startCollectorProcessFixture(t)
	reading := blockedCollectorReading(t, fixture.sampler, func() error {
		fixture.closedQueue = true
		return unix.Close(fixture.sampler.exit.queue)
	})
	assertInvalidCollector(t, reading)

	if reading.failures[0].Stdout == "" || !strings.Contains(reading.failures[0].Cause, "bad file descriptor") {
		t.Fatalf("observer failure masked by collector success: %+v", reading)
	}
}

func assertUntrustedReplyDoesNotContributePeak(t *testing.T, reading processSample) {
	t.Helper()

	var peaks processPeaks
	peaks.record(processSample{descriptors: 5})
	peaks.record(reading)

	if peaks.descriptors != 5 || peaks.descriptorSamples != 1 || peaks.descriptorTerminalSamples != 1 || peaks.sampleAttempts != 2 {
		t.Fatalf("unattributable 96-FD reply contributed a peak: %+v", peaks)
	}
}

func assertOwnedExitRemainsLatched(t *testing.T, sampler *processSampler) {
	t.Helper()

	for range 2 {
		proof, exited, err := sampler.exit.confirm(t.Context(), sampler.pid)
		if err != nil || !exited || proof != "owned_NOTE_EXIT" {
			t.Fatalf("owned exit lost after consuming one-shot event: %q %v %v", proof, exited, err)
		}
	}
}

func startCollectorProcessFixture(t *testing.T) *collectorProcessFixture {
	t.Helper()
	return ownCollectorProcess(t, exec.CommandContext(t.Context(), sleepingChildExecutable, "30"))
}

func ownCollectorProcess(t *testing.T, command *exec.Cmd) *collectorProcessFixture {
	t.Helper()

	sampler, err := prepareProcessSampler()
	if err != nil {
		t.Fatal(err)
	}

	if startErr := command.Start(); startErr != nil {
		t.Fatal(startErr)
	}

	fixture := &collectorProcessFixture{command: command, sampler: sampler}

	t.Cleanup(func() {
		if cleanupErr := fixture.killAndWait(); cleanupErr != nil {
			t.Error(cleanupErr)
		}

		if closeErr := sampler.release(); closeErr != nil && (!fixture.closedQueue || !errors.Is(closeErr, unix.EBADF)) {
			t.Error(closeErr)
		}
	})

	if attachErr := sampler.attach(t.Context(), command.Process.Pid); attachErr != nil {
		t.Fatal(attachErr)
	}

	return fixture
}

func (f *collectorProcessFixture) killAndWait() error {
	if f.waitCalled {
		return nil
	}

	killErr := f.command.Process.Kill()
	f.waitCalled = true

	waitErr := f.command.Wait()
	if waitErr != nil {
		exit, exited := errors.AsType[*exec.ExitError](waitErr)
		if exited && exit.ProcessState != nil {
			waitErr = nil
		}
	}

	return errors.Join(killErr, waitErr)
}

func blockedCollectorReading(t *testing.T, sampler *processSampler, action func() error) processSample {
	t.Helper()
	dir := t.TempDir()

	file := filepath.Join(dir, descriptorCollectorFixtureName)
	if err := os.WriteFile(file, []byte(blockedCollectorScript), executableFixtureMode); err != nil {
		t.Fatal(err)
	}

	sampler.collector = file
	done := make(chan error, 1)

	go func() {
		markerErr := awaitCollectorMarker(t.Context(), filepath.Join(dir, "started"))

		var actionErr error
		if markerErr == nil {
			actionErr = action()
		}

		releaseErr := os.WriteFile(filepath.Join(dir, "released"), []byte("released"), fileMode)
		done <- errors.Join(markerErr, actionErr, releaseErr)
	}()

	reading := sampler.sample(t.Context())
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	return reading
}

func awaitCollectorMarker(ctx context.Context, path string) error {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect collector start marker: %w", err)
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait for collector start marker: %w", err)
		}

		time.Sleep(time.Millisecond)
	}

	return os.ErrDeadlineExceeded
}

func (f *collectorProcessFixture) waitSuccessfully(t *testing.T) {
	t.Helper()

	f.waitCalled = true
	if err := f.command.Wait(); err != nil {
		t.Fatal(err)
	}
}
