// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/plan"
)

// scriptedReader is an input that reports each time the decoder calls Read and then blocks until the test
// gives it a chunk or ends it, so that a test cancels a decoder that is blocked in the read instead of guessing
// how long it takes to get there.
type scriptedReader struct {
	chunks chan []byte
	reads  chan struct{}
}

const (
	// cancelDeadline is how long a canceled decode or an interrupted process may take to end. A decode that
	// never ends would wait forever, so the deadline separates the two without timing anything precisely.
	cancelDeadline = 10 * time.Second
	// startupDeadline is how long a started process may take to reach the state a test waits for.
	startupDeadline = 60 * time.Second
	fixturePackage  = "./internal/plan/testdata/decodecmd"
	exitInterrupt   = 130
)

func newScriptedReader() *scriptedReader {
	return &scriptedReader{chunks: make(chan []byte, 1), reads: make(chan struct{}, 8)}
}

func (r *scriptedReader) Read(p []byte) (int, error) {
	r.reads <- struct{}{}

	chunk, open := <-r.chunks
	if !open {
		return 0, io.EOF
	}

	return copy(p, chunk), nil
}

// waitForReads waits until the notifier has seen count calls to Read.
func waitForReads(t *testing.T, reads <-chan struct{}, count int) {
	t.Helper()

	for range count {
		select {
		case <-reads:
		case <-time.After(startupDeadline):
			t.Fatal("Decode did not read its input")
		}
	}
}

func TestCancelWhileBlockedOnAnOpenReader(t *testing.T) {
	t.Parallel()

	reader := newScriptedReader() // never given a chunk: a blocked standard input
	defer close(reader.chunks)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := plan.Decode(ctx, plan.Input{Name: "<stdin>"}, reader)
		done <- err
	}()

	waitForReads(t, reader.reads, 1)
	cancel()

	select {
	case err := <-done:
		planErr := codedError(t, err, plan.CodeInterrupted)
		if !errors.Is(err, context.Canceled) || planErr.Stage != plan.StageRead {
			t.Fatalf(diagnosticFormat, planErr)
		}
	case <-time.After(cancelDeadline):
		t.Fatal("Decode did not return after cancellation")
	}
}

func TestCancelInTheMiddleOfADocument(t *testing.T) {
	t.Parallel()

	reader := newScriptedReader()
	defer close(reader.chunks)

	ctx, cancel := context.WithCancel(context.Background())

	reader.chunks <- []byte(`{"version":1,"items":["a","b"`)

	done := make(chan error, 1)

	go func() {
		_, err := plan.Decode(ctx, plan.Input{Name: "<stdin>"}, reader)
		done <- err
	}()

	// The first read takes the written part of the document; the second is blocked waiting for the rest.
	waitForReads(t, reader.reads, 2)
	cancel()

	select {
	case err := <-done:
		wantCode(t, err, plan.CodeInterrupted)
	case <-time.After(cancelDeadline):
		t.Fatal("Decode did not return after cancellation")
	}
}

// runInterrupted starts the fixture with an open standard input pipe, writes head if there is one, waits
// until the fixture handles SIGINT and is blocked reading, sends the signal, and returns the exit code and how
// long the process took to exit after the signal. A code of -1 means it was still running after the
// deadline and was killed.
func runInterrupted(t *testing.T, args []string, head string) (int, time.Duration) {
	t.Helper()

	command := exectest.Command(context.Background(), exectest.Build(t, fixturePackage), args...)
	prepareInterruptedProcess(command)

	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	defer closeWithLog(t, input)

	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	err = command.Start()
	if err != nil {
		t.Fatal(err)
	}

	waited := false

	registerFixtureReap(t, command, &waited)

	lines := fixtureLines(output)

	waitForLine(t, command, lines, "ready")

	// The fixture reads before anything is written, so a head is read by the first read and the document
	// is then waiting on the second; without a head the first read is the blocked one.
	reads := 1

	if head != "" {
		_, err = io.WriteString(input, head)
		if err != nil {
			t.Fatal(err)
		}

		reads = 2
	}

	for range reads {
		waitForLine(t, command, lines, "read")
	}

	sent := time.Now()

	err = interruptReaderProcess(t, command)
	if err != nil {
		t.Fatal(err)
	}

	waited = true // awaitExit takes exclusive ownership of the single Wait call.
	code, took := awaitExit(t, command, sent)

	return code, took
}

// fixtureLines delivers the lines the fixture writes to its standard output.
func fixtureLines(output io.Reader) <-chan string {
	lines := make(chan string, 16)

	go func() {
		defer close(lines)

		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	return lines
}

// waitForLine waits for the fixture to write want, killing it if it does not within startupDeadline.
func waitForLine(t *testing.T, command *exec.Cmd, lines <-chan string, want string) {
	t.Helper()

	select {
	case got, open := <-lines:
		if !open || got != want {
			t.Fatalf("the fixture wrote %q (stream open: %v), want %q", got, open, want)
		}
	case <-time.After(startupDeadline):
		killErr := command.Process.Kill()
		if killErr != nil {
			t.Logf("kill: %v", killErr)
		}

		t.Fatalf("the fixture did not write %q", want)
	}
}

func awaitExit(t *testing.T, command *exec.Cmd, sent time.Time) (int, time.Duration) {
	t.Helper()

	done := make(chan error, 1)

	go func() { done <- command.Wait(); close(done) }()

	t.Cleanup(func() {
		killErr := command.Process.Kill()
		if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Logf("kill fixture awaiting exit: %v", killErr)
		}

		<-done
	})

	select {
	case err := <-done:
		took := time.Since(sent)

		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			if exitErr.ExitCode() < 0 {
				t.Fatalf("the fixture was killed by %v instead of handling the interrupt", exitErr)
			}

			return exitErr.ExitCode(), took
		}

		if err != nil {
			t.Fatal(err)
		}

		return 0, took
	case <-time.After(cancelDeadline):
		err := command.Process.Kill()
		if err != nil {
			t.Fatal(err)
		}

		<-done

		return -1, cancelDeadline
	}
}

// TestInterruptWhileBlockedOnStandardInput proves with a real executable that a native console interrupt ends a decode that is
// blocked on an open standard input pipe, promptly and with the interrupted exit status.
func TestInterruptWhileBlockedOnStandardInput(t *testing.T) {
	t.Parallel()

	for name, head := range map[string]string{"nothing written": "", "partial plan written": `{"version":1,"items":["a.pdf"`} {
		code, took := runInterrupted(t, nil, head)
		// The standard input stays open until the test ends, so an exit proves the interrupt, not the end of
		// the input, released the decoder. How fast a loaded machine gets there is not what is proved.
		if code != exitInterrupt {
			t.Errorf("%s: exit %d after %v, want %d", name, code, took, exitInterrupt)
		}
	}
}

// TestNaiveBlockedReadDoesNotExit is the negative control: reading standard input without the decoder's
// cancellable reader stays blocked after a native console interrupt, which is what the real test above guards against.
func TestNaiveBlockedReadDoesNotExit(t *testing.T) {
	t.Parallel()

	code, _ := runInterrupted(t, []string{"naive"}, "")
	if code != -1 {
		t.Fatalf("the naive reader exited with %d; the control no longer demonstrates the stall", code)
	}
}

func registerFixtureReap(t *testing.T, command *exec.Cmd, waitOwned *bool) {
	t.Helper()
	t.Cleanup(func() {
		if *waitOwned {
			return
		}

		killErr := command.Process.Kill()
		if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Logf("kill interrupted fixture: %v", killErr)
		}

		waitErr := command.Wait()
		if waitErr != nil {
			t.Logf("reap interrupted fixture: %v", waitErr)
		}
	})
}
