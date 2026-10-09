// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/internal/cli"
)

const (
	progressOwnedCloseFault  = "PDFCONCAT_IDLE_PROGRESS_DESCRIPTOR_FAULT"
	progressOwnerTestAttempt = "attempt"
)

func TestProgressOwnerRefusesUnsupportedJSONSinkTruthfully(t *testing.T) {
	t.Parallel()
	file, err := os.Create(filepath.Join(t.TempDir(), "stderr.txt"))
	requireProgressNoError(t, err)
	closeProgressResource(t, file)

	owner := &progressOwner{file: file}
	if session := owner.newSession(t.Context(), cli.ProgressJSON, progressOwnerTestAttempt); session != nil || !owner.interrupted() {
		t.Fatal("unsupported file sink did not mark requested telemetry unavailable")
	}

	if err = owner.WriteRecord(t.Context(), []byte(progressTestRecord)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("unconstructed native owner accepted a record: %v", err)
	}

	owner.release()

	stat, err := file.Stat()
	requireProgressNoError(t, err)

	if stat.Size() != 0 {
		t.Fatal("classification refusal wrote into the caller's regular file")
	}
}

func TestProgressOwnerReportsActualIdleDescriptorCloseFault(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs("-test.run=^TestProgressIdleDescriptorCloseFaultHelper$")...)

	command.Env = append(os.Environ(), progressOwnedCloseFault+"=1")

	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("isolated idle ownership-fault injection (watchdog=%v): %v\n%s", ctx.Err(), runErr, output)
	}
}

func TestProgressOwnerDetectsClosedCallerStderr(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs("-test.run=^TestProgressClosedCallerStderrHelper$")...)

	command.Env = append(os.Environ(), progressOwnedCloseFault+"=1")

	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("isolated closed caller stderr (watchdog=%v): %v\n%s", ctx.Err(), runErr, output)
	}
}

func TestProgressClosedCallerStderrHelper(t *testing.T) {
	t.Parallel()

	if os.Getenv(progressOwnedCloseFault) == "" {
		return
	}

	restore, err := unix.FcntlInt(uintptr(unix.Stderr), unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)
	requireProgressNoError(t, err)
	t.Cleanup(func() {
		requireProgressNoError(t, unix.Dup2(restore, unix.Stderr))
		requireProgressNoError(t, unix.Close(restore))
	})
	requireProgressNoError(t, unix.Close(unix.Stderr))
	file := os.NewFile(uintptr(unix.Stderr), "closed-stderr")

	t.Cleanup(func() {
		if closeErr := file.Close(); !errors.Is(closeErr, unix.EBADF) {
			t.Errorf("closed caller wrapper: %v", closeErr)
		}
	})

	owner := &progressOwner{file: file}
	if session := owner.newSession(t.Context(), cli.ProgressJSON, progressOwnerTestAttempt); session != nil || !owner.interrupted() {
		t.Fatal("closed caller stderr did not make selected telemetry unavailable")
	}

	owner.release()
}

func TestProgressIdleDescriptorCloseFaultHelper(t *testing.T) {
	t.Parallel()

	if os.Getenv(progressOwnedCloseFault) == "" {
		return
	}

	reader, writer := progressPipe(t)
	owner := &progressOwner{file: writer}
	t.Cleanup(owner.release)

	session := owner.newSession(t.Context(), cli.ProgressJSON, progressOwnerTestAttempt)
	if session == nil {
		t.Fatal("real pipe did not construct an owned session")
	}

	session.Stop()
	// This is a real ownership-fault injection, not normal supported behavior or
	// a stopped-reader proof. The isolated worker is idle; no descriptor churn occurs.
	requireProgressNoError(t, unix.Close(owner.transport.fd))
	owner.release()

	if !errors.Is(owner.transport.closeErr, unix.EBADF) || !owner.interrupted() {
		t.Fatalf("actual native close failure hidden: %v", owner.transport.closeErr)
	}

	if owner.transport.interruptiblePipe {
		select {
		case <-owner.transport.stopped:
		default:
			t.Fatal("idle native worker was not joined after close failure")
		}
	}

	_, err := writer.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)

	data := make([]byte, len(progressEmptyRecord))
	_, err = io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	if string(data) != progressEmptyRecord {
		t.Fatal("ownership fault affected the caller's original pipe")
	}
}

func TestProgressAutomaticClassificationPreservesGoPipeFlags(t *testing.T) {
	t.Parallel()

	_, writer := progressPipe(t)
	descriptor, err := nativeProgressDescriptor(writer)
	requireProgressNoError(t, err)
	flags := progressDeliveryFlags(t, descriptor)
	owner := &progressOwner{file: writer}

	if owner.newSession(t.Context(), cli.ProgressAuto, progressOwnerTestAttempt) != nil || owner.transport != nil {
		t.Fatal("redirected automatic mode created a native presenter")
	}

	if progressDeliveryFlags(t, descriptor) != flags {
		t.Fatal("automatic terminal detection changed the caller's Go-managed pipe flags")
	}
}

func TestProgressAutomaticClassificationRejectsClosedCaller(t *testing.T) {
	t.Parallel()

	_, writer := progressPipe(t)
	requireProgressNoError(t, writer.Close())
	owner := &progressOwner{file: writer}

	if owner.newSession(t.Context(), cli.ProgressAuto, progressOwnerTestAttempt) != nil ||
		owner.transport != nil || !owner.interrupted() {
		t.Fatal("closed stderr caller was admitted or its unavailable state was hidden")
	}

	if err := owner.WriteRecord(t.Context(), []byte(progressTestRecord)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("refused caller accepted an exceptional record: %v", err)
	}

	owner.release()
}
