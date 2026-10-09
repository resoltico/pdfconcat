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
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type progressPhaseFile struct {
	descriptor uintptr
	status     int
	flags      int
	identity   unix.Stat_t
}

const (
	progressPhaseScenario    = "PDFCONCAT_NATIVE_WRITE_PHASE"
	progressPhaseSelector    = "-test.run=^TestProgressNativeWritePhaseHelper$"
	progressPhaseReceipt     = "native write phase verified: "
	progressPhaseSchedule    = "schedule-canceled"
	progressPhaseNonblocking = "nonblocking-canceled"
	progressPhasePeerClosed  = "terminal-peer-closed"
)

func TestProgressNativeWritePhaseBoundaries(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	for _, scenario := range []string{progressPhaseSchedule, progressPhaseNonblocking, progressPhasePeerClosed} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			command := exec.CommandContext(ctx, executable, progressHelperArgs(progressPhaseSelector, progressNativeVerbose)...)

			command.Env = append(os.Environ(), progressPhaseScenario+"="+scenario)
			assertProgressNativeChild(ctx, t, command, progressPhaseReceipt+scenario, "TestProgressNativeWritePhaseHelper")
		})
	}
}

func TestProgressNativeWritePhaseHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressPhaseScenario)
	if scenario == "" {
		return
	}

	switch scenario {
	case progressPhaseSchedule:
		assertProgressCanceledSchedulingPhase(t)
	case progressPhaseNonblocking:
		assertProgressCanceledNonblockingPhase(t)
	case progressPhasePeerClosed:
		assertProgressTerminalPeerFailure(t)
	default:
		t.Fatal("unknown native write phase")
	}

	t.Log(progressPhaseReceipt + scenario)
}

// The worker is genuinely occupied by a native full-pipe write. This tests the scheduling phase,
// not the public method's earlier admission check for an already canceled context.
func assertProgressCanceledSchedulingPhase(t *testing.T) {
	t.Helper()

	reader, source := progressPipe(t)
	// A genuine first write sets Darwin's kernel-maintained written status.
	// Establish that ordinary activity before checking full flag preservation.
	_, warmErr := source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, warmErr)
	assertProgressPhaseBytes(t, reader, progressEmptyRecord)

	caller := captureProgressPhaseFile(t, source)
	requireProgressNoError(t, unix.SetNonblock(int(caller.descriptor), true))
	padding := fillProgressNativeSink(t, int(caller.descriptor))
	_, err := unix.FcntlInt(caller.descriptor, unix.F_SETFL, caller.status)
	requireProgressNoError(t, err)

	transport := progressNativeTransport(t, source)
	// Breaking the real pipe on a failing assertion lets the worker join before transport cleanup.
	closeProgressResource(t, reader)

	result := make(chan error, 1)
	transport.requests <- progressWrite{record: []byte(progressTestRecord), result: result}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	attempted, err := transport.writeInterruptiblePipe(ctx, []byte(progressEmptyRecord))
	if attempted || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "schedule progress write") ||
		transport.Interrupted() {
		t.Fatalf("canceled scheduling changed native state: attempted=%v interrupted=%v error=%v", attempted, transport.Interrupted(), err)
	}

	data := make([]byte, padding+len(progressTestRecord))
	_, err = io.ReadFull(reader, data)
	requireProgressNoError(t, err)
	requireProgressNoError(t, <-result)

	if !bytes.Equal(data[:padding], make([]byte, padding)) || string(data[padding:]) != progressTestRecord {
		t.Fatal("scheduling cancellation changed actual padding or completed worker record")
	}

	assertProgressPhaseEmpty(t, reader)
	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressTestRecord)))
	assertProgressPhaseBytes(t, reader, progressTestRecord)
	requireProgressNoError(t, transport.Close())
	requireRelocationVacancy(t, transport.fd)
	requireRelocationVacancy(t, transport.guard)
	assertProgressPhaseFile(t, source, caller)
	_, err = source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)
	assertProgressPhaseBytes(t, reader, progressEmptyRecord)
}

func assertProgressCanceledNonblockingPhase(t *testing.T) {
	t.Helper()

	master, source := progressPhasePTY(t)
	caller := captureProgressPhaseFile(t, source)
	modes := progressPollTermios(t, int(caller.descriptor), progressPollTerminal)
	transport := progressNativeTransport(t, source)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// This is the nonblocking phase directly, not public preadmission cancellation.
	attempted, err := transport.writeNonblocking(ctx, []byte(progressEmptyRecord))
	if attempted || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "progress write deadline") ||
		transport.Interrupted() {
		t.Fatalf("nonblocking cancellation: attempted=%v interrupted=%v error=%v", attempted, transport.Interrupted(), err)
	}

	assertProgressPhaseEmpty(t, master)
	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressTestRecord)))
	assertProgressPhaseBytes(t, master, progressTestRecord)
	requireProgressNoError(t, transport.Close())
	requireRelocationVacancy(t, transport.fd)
	assertProgressPhaseFile(t, source, caller)

	afterModes := progressPollTermios(t, int(caller.descriptor), progressPollTerminal)
	if *afterModes != *modes {
		t.Fatal("nonblocking phase changed caller terminal modes")
	}

	_, err = source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)
	assertProgressPhaseBytes(t, master, progressEmptyRecord)
}

func assertProgressPhaseEmpty(t *testing.T, reader *os.File) {
	t.Helper()

	descriptor, err := nativeProgressDescriptor(reader)
	requireProgressNoError(t, err)
	status, err := unix.FcntlInt(descriptor, unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	requireProgressNoError(t, unix.SetNonblock(int(descriptor), true))

	var data [1]byte

	count, readErr := unix.Read(int(descriptor), data[:])
	_, restoreErr := unix.FcntlInt(descriptor, unix.F_SETFL, status)
	requireProgressNoError(t, restoreErr)

	if count > 0 || !errors.Is(readErr, unix.EAGAIN) {
		t.Fatalf("canceled phase emitted extra native bytes: %d/%v", count, readErr)
	}
}

func assertProgressTerminalPeerFailure(t *testing.T) {
	t.Helper()

	master, source := progressPhasePTY(t)
	caller := captureProgressPhaseFile(t, source)
	transport := progressNativeTransport(t, source)
	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressTestRecord)))
	assertProgressPhaseBytes(t, master, progressTestRecord)
	masterDescriptor, err := nativeProgressDescriptor(master)
	requireProgressNoError(t, err)
	requireProgressNoError(t, master.Close())
	requireRelocationVacancy(t, int(masterDescriptor))

	attempted, writeErr := transport.writeNonblocking(t.Context(), []byte(progressEmptyRecord))
	if !attempted || !errors.Is(writeErr, unix.EIO) || !strings.Contains(writeErr.Error(), "write progress sink") {
		t.Fatalf("closed PTY peer did not cause real native EIO: attempted=%v error=%v", attempted, writeErr)
	}

	err = transport.WriteRecord(t.Context(), []byte(progressEmptyRecord))
	if !errors.Is(err, unix.EIO) || !transport.Interrupted() {
		t.Fatalf("actual native peer failure did not retire public transport: %v", err)
	}

	assertProgressPoisoned(t.Context(), t, transport)
	requireProgressNoError(t, transport.Close())
	requireRelocationVacancy(t, transport.fd)
	assertProgressPhaseFile(t, source, caller)
}

func progressPhasePTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	master, source := progressNativePTYPair(t)
	descriptor, err := nativeProgressDescriptor(source)
	requireProgressNoError(t, err)
	modes, err := unix.IoctlGetTermios(int(descriptor), unix.TIOCGETA)
	requireProgressNoError(t, err)

	modes.Oflag &^= unix.OPOST
	requireProgressNoError(t, unix.IoctlSetTermios(int(descriptor), unix.TIOCSETA, modes))

	return master, source
}

func captureProgressPhaseFile(t *testing.T, file *os.File) progressPhaseFile {
	t.Helper()

	descriptor, err := nativeProgressDescriptor(file)
	requireProgressNoError(t, err)

	result := progressPhaseFile{descriptor: descriptor}
	result.status, err = unix.FcntlInt(descriptor, unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	result.flags, err = unix.FcntlInt(descriptor, unix.F_GETFD, 0)
	requireProgressNoError(t, err)
	requireProgressNoError(t, unix.Fstat(int(descriptor), &result.identity))

	return result
}

func assertProgressPhaseFile(t *testing.T, file *os.File, before progressPhaseFile) {
	t.Helper()

	after := captureProgressPhaseFile(t, file)
	if after.descriptor != before.descriptor || after.status != before.status || after.flags != before.flags ||
		after.identity.Dev != before.identity.Dev || after.identity.Ino != before.identity.Ino ||
		after.identity.Mode&unix.S_IFMT != before.identity.Mode&unix.S_IFMT {
		t.Fatalf("native write phase changed caller: fd %d/%d status %#x/%#x flags %#x/%#x identity %d:%d/%d:%d mode %#x/%#x",
			before.descriptor, after.descriptor, before.status, after.status, before.flags, after.flags,
			before.identity.Dev, before.identity.Ino, after.identity.Dev, after.identity.Ino,
			before.identity.Mode&unix.S_IFMT, after.identity.Mode&unix.S_IFMT)
	}
}

func assertProgressPhaseBytes(t *testing.T, reader *os.File, expected string) {
	t.Helper()

	data := make([]byte, len(expected))
	_, err := io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	if string(data) != expected {
		t.Fatalf("actual native record bytes changed: %q", data)
	}
}
