// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type progressJoinPipe struct {
	reader    *os.File
	source    *os.File
	transport *progressTransport
	caller    progressPhaseFile
	padding   int
}

const (
	progressJoinScenario = "PDFCONCAT_NATIVE_PIPE_JOIN"
	progressJoinSelector = "-test.run=^TestProgressNativePipeJoinHelper$"
	progressJoinReceipt  = "native completed pipe write joined after deadline cancellation"
)

func TestProgressNativePipeCompletionAfterCancellation(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs(progressJoinSelector, progressNativeVerbose)...)

	command.Env = append(os.Environ(), progressJoinScenario+"=1")
	assertProgressNativeChild(ctx, t, command, progressJoinReceipt, "TestProgressNativePipeJoinHelper")
}

func TestProgressNativePipeJoinHelper(t *testing.T) {
	t.Parallel()

	if os.Getenv(progressJoinScenario) == "" {
		return
	}

	if os.Getenv(progressJoinScenario) != "1" {
		t.Fatal("unknown native pipe join scenario")
	}

	assertProgressCancellationJoin(t)
	t.Log(progressJoinReceipt)
}

func prepareProgressJoinPipe(t *testing.T) progressJoinPipe {
	t.Helper()

	reader, source := progressPipe(t)
	// Warm the caller's genuine kernel write status before measuring complete flags.
	_, warmErr := source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, warmErr)
	assertProgressPhaseBytes(t, reader, progressEmptyRecord)
	caller := captureProgressPhaseFile(t, source)
	requireProgressNoError(t, unix.SetNonblock(int(caller.descriptor), true))
	padding := fillProgressNativeSink(t, int(caller.descriptor))
	_, err := unix.FcntlInt(caller.descriptor, unix.F_SETFL, caller.status)
	requireProgressNoError(t, err)
	transport := progressNativeTransport(t, source)
	// Reader retirement must precede transport cleanup if an assertion fails with a blocked worker.
	closeProgressResource(t, reader)

	return progressJoinPipe{reader: reader, source: source, transport: transport, caller: caller, padding: padding}
}

func assertProgressCancellationJoin(t *testing.T) {
	t.Helper()

	pipe := prepareProgressJoinPipe(t)

	guard, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	deadline, ok := guard.Deadline()
	if !ok {
		t.Fatal("native join guard has no deadline")
	}

	requireProgressNoError(t, pipe.reader.SetReadDeadline(deadline))

	result := make(chan error, 1)
	start := make(chan struct{})
	finished := make(chan struct{})
	writeContext := t.Context()
	record := []byte(progressTestRecord)
	barrierResult := make(chan error, 1)
	// This empty internal request only observes the real worker's previous completion.
	barrier := progressWrite{record: nil, result: barrierResult}
	data := make([]byte, pipe.padding+len(progressTestRecord))
	expectedPadding := make([]byte, pipe.padding)
	unlock := sync.OnceFunc(syscall.ForkLock.RUnlock)

	go func() {
		defer close(finished)

		<-start

		result <- pipe.transport.WriteRecord(writeContext, record)
	}()

	registerProgressJoinCleanup(t, pipe.reader, finished)

	syscall.ForkLock.RLock()
	// This unconditional defer releases the lock BEFORE any transport or reader cleanup joins.
	defer unlock()

	close(start)

	// The production write deadline supplies genuine cancellation. Public Interrupted proves the
	// accepted request reached retirement; the actual ForkLock prevents descriptor replacement.
	waitProgressJoinRetirement(guard, t, pipe.transport)

	_, err := io.ReadFull(pipe.reader, data)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data[:pipe.padding], expectedPadding) || string(data[pipe.padding:]) != progressTestRecord {
		t.Fatal("cancellation join changed actual native padding or completed record")
	}

	joinProgressWorkerBarrier(guard, t, pipe.transport, barrier)

	// Only the real worker can accept the barrier after publishing the completed write's result.
	unlock()
	requireProgressJoinResult(guard, t, result)
	assertProgressJoinedPipeCleanup(t, pipe)
}

func assertProgressJoinedPipeCleanup(t *testing.T, pipe progressJoinPipe) {
	t.Helper()

	if !pipe.transport.Interrupted() {
		t.Fatal("completed cancellation write lost retirement state")
	}

	assertProgressPoisoned(t.Context(), t, pipe.transport)
	requireProgressNoError(t, pipe.transport.Close())
	requireRelocationVacancy(t, pipe.transport.fd)
	requireRelocationVacancy(t, pipe.transport.guard)
	assertProgressPhaseFile(t, pipe.source, pipe.caller)
	assertProgressPhaseEmpty(t, pipe.reader)

	_, err := pipe.source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)
	assertProgressPhaseBytes(t, pipe.reader, progressEmptyRecord)
	requireProgressNoError(t, pipe.reader.SetReadDeadline(time.Time{}))
}

func waitProgressJoinRetirement(guard context.Context, t *testing.T, transport *progressTransport) {
	t.Helper()

	for !transport.Interrupted() {
		select {
		case <-guard.Done():
			t.Fatal("real full-pipe request did not reach public cancellation retirement")
		default:
			runtime.Gosched()
		}
	}

	select {
	case <-transport.closed:
	case <-guard.Done():
		t.Fatal("public Interrupted did not finish retiring native write admission")
	}
}

func joinProgressWorkerBarrier(guard context.Context, t *testing.T, transport *progressTransport, barrier progressWrite) {
	t.Helper()

	select {
	case transport.requests <- barrier:
	case <-guard.Done():
		t.Fatal("real native worker did not accept completion barrier")
	}

	requireProgressJoinResult(guard, t, barrier.result)
}

func requireProgressJoinResult(guard context.Context, t *testing.T, result <-chan error) {
	t.Helper()

	select {
	case err := <-result:
		requireProgressNoError(t, err)
	case <-guard.Done():
		t.Fatal("real native write did not join within its independent guard")
	}
}

func registerProgressJoinCleanup(t *testing.T, reader *os.File, finished <-chan struct{}) {
	t.Helper()

	t.Cleanup(func() {
		// The controller's defer has released ForkLock before this failure rescue and join.
		if closeErr := reader.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Error(closeErr)
		}

		select {
		case <-finished:
			return
		default:
		}

		// Test/function contexts are already canceled during cleanup; this independent guard
		// bounds only the genuine writer join after reader retirement and lock release.
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		select {
		case <-finished:
		case <-cleanup.Done():
			t.Error("native public writer did not finish during cleanup")
		}
	})
}
