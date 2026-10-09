// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package scale

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

type descriptorFixture struct {
	command    *exec.Cmd
	sampler    *processSampler
	input      io.WriteCloser
	output     *bufio.Reader
	waitCalled bool
}

const descriptorControlScript = `while IFS= read -r command; do
case "$command" in
status) printf 'status\n';;
open) exec 9</dev/null; printf 'opened\n';;
close) exec 9<&-; printf 'closed\n';;
exit) exit 0;;
esac
done`

func TestOwnedProcDirectoryResetsAndObservesLiveDescriptorChanges(t *testing.T) {
	t.Parallel()
	fixture := startDescriptorFixture(t)
	sampler, input, reader := fixture.sampler, fixture.input, fixture.output
	descriptorControl(t, input, reader, "status", "status")

	baseline := sampler.sample(t.Context())
	if baseline.descriptors <= 0 {
		t.Fatalf("missing live baseline: %+v", baseline)
	}

	descriptorControl(t, input, reader, "open", "opened")
	assertDescriptorCount(t, sampler.sample(t.Context()), baseline.descriptors+1)
	assertDescriptorCount(t, sampler.sample(t.Context()), baseline.descriptors+1)
	descriptorControl(t, input, reader, "close", "closed")
	assertDescriptorCount(t, sampler.sample(t.Context()), baseline.descriptors)

	if _, writeErr := fmt.Fprintln(input, "exit"); writeErr != nil {
		t.Fatal(writeErr)
	}

	awaitUnreapedZero(t, sampler)

	fixture.waitCalled = true
	if waitErr := fixture.command.Wait(); waitErr != nil {
		t.Fatal(waitErr)
	}

	terminal := sampler.sample(t.Context())
	if !terminal.terminal || terminal.descriptors != -1 || len(terminal.failures) != 1 ||
		terminal.failures[0].Probe != "owned_proc_inode_task_gone: ENOENT" {
		t.Fatalf("reaped owned inode not identified: %+v", terminal)
	}

	assertReadingFailureEvidence(t, terminal.failures)
}

func descriptorControl(t *testing.T, input io.Writer, output *bufio.Reader, command, want string) {
	t.Helper()

	if _, err := fmt.Fprintln(input, command); err != nil {
		t.Fatal(err)
	}

	line, err := output.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	if line != want+"\n" {
		t.Fatalf("descriptor control reply: %q want %q", line, want)
	}
}

func assertDescriptorCount(t *testing.T, sample processSample, want int64) {
	t.Helper()

	if sample.descriptors != want || sample.terminal || len(sample.failures) != 0 {
		t.Fatalf("owned descriptor reading: %+v want %d", sample, want)
	}
}

func waitDescriptorChild(t *testing.T, child *exec.Cmd) {
	t.Helper()

	err := child.Wait()
	if err == nil {
		return
	}

	exit, exited := errors.AsType[*exec.ExitError](err)
	if !exited || exit.ProcessState == nil {
		t.Error(err)
	}
}

func TestOwnedProcDirectoryClosedHandleIsNotTerminal(t *testing.T) {
	t.Parallel()

	sampler, prepareErr := prepareProcessSampler(t.Context())
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}

	if err := sampler.attach(t.Context(), os.Getpid()); err != nil {
		t.Fatal(err)
	}

	if err := sampler.directory.Close(); err != nil {
		t.Fatal(err)
	}

	sample := sampler.sample(t.Context())
	if sample.terminal || len(sample.failures) != 1 || sample.failures[0].Phase != "live_error" {
		t.Fatalf("closed handle mistaken for exit: %+v", sample)
	}

	if err := sampler.release(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("close failure lost: %v", err)
	}

	if err := sampler.release(); err != nil {
		t.Fatal(err)
	}
}

func startDescriptorFixture(t *testing.T) *descriptorFixture {
	t.Helper()
	child := exec.CommandContext(t.Context(), "/bin/sh", "-c", descriptorControlScript)

	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if startErr := child.Start(); startErr != nil {
		t.Fatal(startErr)
	}

	fixture := &descriptorFixture{command: child, input: input, output: bufio.NewReader(output)}

	t.Cleanup(func() {
		if !fixture.waitCalled {
			if killErr := child.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				t.Error(killErr)
			}

			waitDescriptorChild(t, child)
		}
	})

	sampler, prepareErr := prepareProcessSampler(t.Context())
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}

	if attachErr := sampler.attach(t.Context(), child.Process.Pid); attachErr != nil {
		t.Fatal(attachErr)
	}

	t.Cleanup(func() {
		if closeErr := sampler.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	fixture.sampler = sampler

	return fixture
}

func awaitUnreapedZero(t *testing.T, sampler *processSampler) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sample := sampler.sample(t.Context())
		if sample.terminal || len(sample.failures) != 0 {
			t.Fatalf("unreaped child descriptor reading failed: %+v", sample)
		}

		if sample.descriptors == 0 {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("owned unreaped process never yielded known zero")
}
