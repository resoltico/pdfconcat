// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package main_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// interruptDeadline is how long a command may take to stop after a signal.
	interruptDeadline = 10 * time.Second
	// startedReading is how long a command is given to reach a blocked read of standard input before it is signaled.
	startedReading = time.Second
	// resendInterval is how often a signal is sent again to a command that has not stopped.
	resendInterval = time.Second
)

// killAndFail ends the command, which is stuck, and fails the test with reason.
func killAndFail(tb testing.TB, command *exec.Cmd, reason string, args ...any) {
	tb.Helper()

	killErr := command.Process.Kill()

	tb.Fatalf(reason+" (kill: %v)", append(args, killErr)...)
}

// ignoringSignals makes command start with SIGINT and SIGTERM ignored. A Go program started that way
// ignores them until it asks for them, which the executable does first thing in main, so a signal sent
// to a process that is still starting up is dropped instead of killing it.
func ignoringSignals(tb testing.TB, command *exec.Cmd) {
	tb.Helper()

	shell, err := exec.LookPath("sh")
	ensure(tb, err)

	command.Args = append([]string{"sh", "-c", `trap "" INT TERM; exec "$0" "$@"`, command.Path}, command.Args[1:]...)
	command.Path = shell
}

// interrupted runs the command until it ends or interruptDeadline passes after signal was first sent. The
// signal is sent again every resendInterval while the command runs: ready is a guess about timing that a
// loaded machine can get wrong, and a signal that arrives before the executable listens for it is ignored,
// so repeating it delivers it once the executable does. Once the executable listens, the first signal ends
// it with the interrupted status.
func interrupted(tb testing.TB, command *exec.Cmd, signal os.Signal, ready func() bool) result {
	tb.Helper()

	ignoringSignals(tb, command)

	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr

	ensure(tb, command.Start())

	done := make(chan error, 1)

	go func() { done <- command.Wait() }()

	deadline := time.Now().Add(commandTimeout)
	for !ready() {
		if time.Now().After(deadline) {
			killAndFail(tb, command, "the command never reached the point where it is signaled")
		}

		select {
		case err := <-done:
			tb.Fatalf("the command ended before it was signaled: %v\nstdout: %.400s\nstderr: %.400s", err, stdout.String(), stderr.String())
		case <-time.After(time.Millisecond):
		}
	}

	began := time.Now()

	for time.Since(began) < interruptDeadline {
		// The command may have ended since the last look; a signal to it then fails and is not a finding.
		err := command.Process.Signal(signal)
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			tb.Fatal(err)
		}

		select {
		case waitErr := <-done:
			return outcome(tb, waitErr, &stdout, &stderr, tempOf(command))
		case <-time.After(resendInterval):
		}
	}

	killAndFail(tb, command, "the command did not stop within %v of %v", interruptDeadline, signal)

	return result{}
}

func TestSignalDuringBlockedStandardInput(t *testing.T) {
	t.Parallel()

	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		dir := tempDir(t)
		writePDFs(t, dir, 1, "a")

		command := start(t, dir, commandBuild, flagPlan, "-", "-o", fileOut)

		// A plan that is never finished, on a pipe that is never closed.
		pipe, err := command.StdinPipe()
		ensure(t, err)

		_, err = pipe.Write([]byte(`{"version":1,`))
		ensure(t, err)

		began := time.Now()
		res := interrupted(t, command, signal, func() bool { return time.Since(began) > startedReading })

		requireExit(t, res, 130)

		parsed := summaryOf(t, res)
		if parsed.Status != statusInterrupted || parsed.Diagnostics[0].Code != statusInterrupted {
			t.Errorf("%v: %+v", signal, parsed)
		}

		requireAbsent(t, filepath.Join(dir, fileOut))
		requireNoScratch(t, dir)
	}
}

// Named FIFOs without writers must be rejected before any read or signal is needed.
func TestNamedFIFOWithoutWriterIsRejectedPromptly(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{commandCheck, flagPlan, pipePath}, {commandReport, pipePath}} {
		dir := tempDir(t)
		ensure(t, syscall.Mkfifo(filepath.Join(dir, pipePath), 0o600))

		// Building the executable is setup; the deadline measures its FIFO rejection.
		command := start(t, dir, args...)
		began := time.Now()
		res := finish(t, command)
		requireExit(t, res, 1)

		if time.Since(began) > 5*time.Second {
			t.Fatal("named FIFO open blocked")
		}
	}
}

// TestReportFromAPipeIsRejected: a saved report is read to the end before it is decoded, so a pipe that is
// never closed must be refused up front instead of blocking the command.
func TestReportFromAPipeIsRejected(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	pipe := filepath.Join(dir, pipePath)
	ensure(t, syscall.Mkfifo(pipe, 0o600))

	writer, err := os.OpenFile(filepath.Clean(pipe), os.O_RDWR, 0)
	ensure(t, err)

	t.Cleanup(func() { ensure(t, writer.Close()) })

	res := run(t, dir, "", commandReport, pipePath)

	requireExit(t, res, 1)

	parsed := summaryOf(t, res)
	if len(parsed.Diagnostics) != 1 || parsed.Diagnostics[0].Code != "report_read_failed" {
		t.Errorf(summaryFailureFormat, parsed)
	}
}

func TestSignalDuringCaptureOfALargeSource(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a")

	// A sparse file: cheap to create, but copying it takes long enough to be interrupted in the middle.
	writeSparseFile(t, filepath.Join(dir, "huge.pdf"), 768<<20)

	command := start(t, dir, commandCheck, fileA, "huge.pdf")
	scratch := tempOf(command)

	res := interrupted(t, command, os.Interrupt, func() bool { return scratchBytes(scratch) > 4<<20 })

	requireExit(t, res, 130)

	parsed := summaryOf(t, res)
	if parsed.Status != statusInterrupted {
		t.Errorf(summaryFailureFormat, parsed)
	}

	requireNoScratch(t, scratch)
}

// scratchBytes is the total size of the files in job workspaces below dir.
func scratchBytes(dir string) int64 {
	var total int64

	matches, err := filepath.Glob(filepath.Join(dir, "pdfconcat-job-*", "*"))
	if err != nil {
		return 0
	}

	for _, match := range matches {
		if info, statErr := os.Stat(match); statErr == nil {
			total += info.Size()
		}
	}

	return total
}

func TestBrokenStandardOutputAfterPublication(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)
	writePDFs(t, dir, 1, "a", "b")

	reader, writer, err := os.Pipe()
	ensure(t, err)
	ensure(t, reader.Close()) // nobody will ever read standard output

	var stderr bytes.Buffer

	command := start(t, dir, commandBuild, "-o", fileOut, flagReport, shortReportPath, fileA, fileB)
	command.Stdout, command.Stderr = writer, &stderr

	runErr := command.Run()
	closeErr := writer.Close()

	ensure(t, closeErr)

	exitErr, ok := errors.AsType[*exec.ExitError](runErr)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("exit: %v (stderr %q); a broken standard output must end the command with 1, not a signal", runErr, stderr.String())
	}

	// The PDF and the report were committed, and standard error says so, in one bounded JSON record.
	verifyPages(t, filepath.Join(dir, fileOut), sourceAMarker, sourceBFirstMarker)

	state := generic(t, stderr.String())
	committed := state["kind"] == "committed_state" && flagAt(t, state, "published") && state["report_status"] == reportWritten

	if !committed || state["status"] != "ok" || !strings.HasSuffix(textAt(t, state, keyOutput), fileOut) {
		t.Errorf("committed state: %v", state)
	}

	if stderr.Len() > 2048 {
		t.Errorf("committed state is %d bytes", stderr.Len())
	}

	requireNoScratch(t, dir)
}

func TestBrokenStreamsDoNotPanic(t *testing.T) {
	t.Parallel()

	dir := tempDir(t)

	for _, args := range [][]string{{commandVersion}, {commandHelp}, {"schema", planSchemaName}, {commandCheck, fileMissing}, {"bogus"}} {
		out, outWriter, err := os.Pipe()
		ensure(t, err)
		ensure(t, out.Close())

		errReader, errWriter, err := os.Pipe()
		ensure(t, err)
		ensure(t, errReader.Close())

		command := start(t, dir, args...)
		command.Stdout, command.Stderr = outWriter, errWriter

		runErr := command.Run()

		ensure(t, outWriter.Close())
		ensure(t, errWriter.Close())

		exitErr, ok := errors.AsType[*exec.ExitError](runErr)
		if !ok || exitErr.ExitCode() < 1 || exitErr.ExitCode() > 2 {
			t.Errorf("%v: %v; want exit 1 or 2, not a signal or panic", args, runErr)
		}
	}
}
