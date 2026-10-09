// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

type acknowledgedCollectorChild struct {
	input   io.WriteCloser
	output  *bufio.Reader
	command *exec.Cmd
	waited  bool
}

const ownedExitProof = "owned_NOTE_EXIT"

func startAcknowledgedCollectorChild(ctx context.Context, t *testing.T) *acknowledgedCollectorChild {
	t.Helper()

	command := exec.CommandContext(ctx, controlledShellExecutable, "-c", `read first; printf 'alive\n'; read release`)

	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	child := &acknowledgedCollectorChild{input: input, output: bufio.NewReader(output), command: command}

	if err = command.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if child.waited {
			return
		}

		if killErr := command.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Error(killErr)
		}

		if waitErr := command.Wait(); waitErr == nil {
			t.Error("unexpected successful exit after cleanup kill")
		}
	})

	return child
}

func (child *acknowledgedCollectorChild) acknowledgeAndExit(t *testing.T) {
	t.Helper()

	if _, err := fmt.Fprintln(child.input, "acknowledge"); err != nil {
		t.Fatal(err)
	}

	if ack, err := child.output.ReadString('\n'); err != nil || ack != "alive\n" {
		t.Fatalf("same child did not acknowledge after collector failure: %q, %v", ack, err)
	}

	if _, err := fmt.Fprintln(child.input, "release"); err != nil {
		t.Fatal(err)
	}

	err := child.command.Wait()
	child.waited = true

	if err != nil {
		t.Fatal(err)
	}
}

func TestFailedDescriptorQueryRemainsFailedAfterAcknowledgedChildExit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	child := startAcknowledgedCollectorChild(ctx, t)

	sampler, err := prepareProcessSampler(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err = sampler.attach(ctx, child.command.Process.Pid); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if releaseErr := sampler.release(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})

	started := time.Now()
	raw, queryErr := exectest.Command(ctx, failedCollectorExecutable).Output()
	finished := time.Now()

	if queryErr == nil {
		t.Fatal("real failed collector unexpectedly succeeded")
	}

	child.acknowledgeAndExit(t)

	reading := sampler.failedCollectorRead(ctx, started, finished, raw, queryErr)
	assertFailedCollectorObservation(t, reading)
}

func assertFailedCollectorObservation(t *testing.T, reading processSample) {
	t.Helper()

	if reading.terminal || reading.descriptors != -1 || len(reading.failures) != 1 ||
		reading.failures[0].Phase != phaseLiveError || reading.failures[0].Probe != ownedExitProof {
		t.Fatalf("failed query laundered by later real exit: %+v", reading)
	}

	var peaks processPeaks

	peaks.record(reading)

	_, state := peaks.states(1)

	if state != failedReading || peaks.descriptorSamples != 0 || peaks.terminalSamples != 0 {
		t.Fatalf("failed observation became successful evidence: %+v state=%s", peaks, state)
	}
}
