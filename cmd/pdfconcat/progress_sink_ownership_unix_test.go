// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	progressSinkOwnershipScenario  = "PDFCONCAT_PROGRESS_SINK_OWNERSHIP"
	progressSinkOwnershipOperation = "PDFCONCAT_PROGRESS_SINK_OPERATION"
)

func TestProgressSinkCleanupPreservesBorrowedStandardDescriptors(t *testing.T) {
	verifyProgressSinkOwnership(t, "cleanup")
}

func TestProgressSinkConstructorRejectsMalformedAcquisitions(t *testing.T) {
	verifyProgressSinkOwnership(t, "construct")
}

func verifyProgressSinkOwnership(t *testing.T, operation string) {
	t.Helper()
	t.Parallel()
	for _, scenario := range []string{
		"zero", "borrowed-target", "borrowed-output-target", "borrowed-error-target",
		"borrowed-input-guard", "borrowed-guard", "borrowed-error-guard", "duplicate",
		"unrepresentable-target", "unrepresentable-guard",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			trace, err := runProgressPhaseChild(t, "TestProgressSinkOwnershipHelper", []string{progressSinkOwnershipScenario + "=" + scenario, progressSinkOwnershipOperation + "=" + operation}, 3*time.Second, 10*time.Second)
			if err != nil {
				t.Fatalf("private sink ownership boundary: %v; %s", err, trace)
			}
			t.Log(trace)
		})
	}
}

func TestProgressSinkOwnershipHelper(t *testing.T) {
	scenario := os.Getenv(progressSinkOwnershipScenario)
	if scenario == "" {
		return
	}
	control := progressPhaseControl(t)
	// Restore borrowed descriptors before assertions or Go's final test output,
	// even if the negative control closes stdout or stderr.
	var before [3]unix.Stat_t
	var beforeFlags, beforeStatus [3]int
	var saved [3]int
	for descriptor := range saved {
		requireProgressNoError(t, unix.Fstat(descriptor, &before[descriptor]))
		var err error
		beforeFlags[descriptor], err = unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		requireProgressNoError(t, err)
		beforeStatus[descriptor], err = unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
		requireProgressNoError(t, err)
		saved[descriptor], err = unix.FcntlInt(uintptr(descriptor), unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)
		requireProgressNoError(t, err)
	}
	t.Cleanup(func() {
		for descriptor, restore := range saved {
			requireProgressNoError(t, unix.Dup2(restore, descriptor))
			_, err := unix.FcntlInt(uintptr(descriptor), unix.F_SETFD, beforeFlags[descriptor])
			requireProgressNoError(t, err)
			requireProgressNoError(t, unix.Close(restore))
		}
	})
	owned, err := unix.Open("/dev/null", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	requireProgressNoError(t, err)
	if owned < progressMinimumOwnedDescriptor {
		t.Fatal("private fixture acquired borrowed descriptor")
	}
	t.Cleanup(func() {
		var stat unix.Stat_t
		if owned >= progressMinimumOwnedDescriptor && unix.Fstat(owned, &stat) == nil {
			requireProgressNoError(t, unix.Close(owned))
		}
	})
	sink := progressNativeSink{}
	wideDescriptor := uint64(1) << 32 // Native close accepts only the low 32 descriptor bits.
	switch scenario {
	case "zero":
	case "borrowed-target":
		sink.guard = owned
	case "borrowed-output-target":
		sink.fd, sink.guard = unix.Stdout, owned
	case "borrowed-error-target":
		sink.fd, sink.guard = unix.Stderr, owned
	case "borrowed-input-guard":
		sink.fd, sink.guard = owned, unix.Stdin
	case "borrowed-guard":
		sink.fd, sink.guard = owned, unix.Stdout
	case "borrowed-error-guard":
		sink.fd, sink.guard = owned, unix.Stderr
	case "duplicate":
		sink.fd, sink.guard = owned, owned
	case "unrepresentable-target":
		sink.fd, sink.guard = int(wideDescriptor), owned
	case "unrepresentable-guard":
		sink.fd, sink.guard = owned, int(wideDescriptor)+unix.Stderr
	default:
		t.Fatal("unknown private sink ownership scenario")
	}
	if _, err := fmt.Fprintln(control, "ready"); err != nil {
		t.Fatal(err)
	}
	var closeErr error
	accepted := false
	if os.Getenv(progressSinkOwnershipOperation) == "construct" {
		// A malformed successful acquisition must fail before a worker is admitted.
		sink.interruptiblePipe = true
		var transport *progressTransport
		transport, closeErr = progressTransportFromSink(sink)
		if transport != nil {
			accepted = true
			closeErr = errors.Join(closeErr, transport.Close())
		}
	} else {
		transport := &progressTransport{progressTransportState: &progressTransportState{fd: sink.fd, guard: sink.guard}}
		transport.closeNativeProgress()
		closeErr = transport.closeErr
	}
	var ownedStat unix.Stat_t
	ownedErr := unix.Fstat(owned, &ownedStat)
	if errors.Is(ownedErr, unix.EBADF) {
		owned = -1 // Cleanup no longer owns this slot, even if logging reuses it.
	}
	var after [3]unix.Stat_t
	var afterFlags, afterStatus [3]int
	var inspect [3]error
	for descriptor := range after {
		inspect[descriptor] = unix.Fstat(descriptor, &after[descriptor])
		var err error
		afterFlags[descriptor], err = unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		inspect[descriptor] = errors.Join(inspect[descriptor], err)
		afterStatus[descriptor], err = unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
		inspect[descriptor] = errors.Join(inspect[descriptor], err)
	}
	// Restore synchronously before reporting findings about a refused borrowed sink.
	for descriptor, restore := range saved {
		requireProgressNoError(t, unix.Dup2(restore, descriptor))
		_, err := unix.FcntlInt(uintptr(descriptor), unix.F_SETFD, beforeFlags[descriptor])
		requireProgressNoError(t, err)
	}
	for descriptor := range after {
		// Shared stream activity may change stat timestamps and byte counts;
		// ownership is its native identity plus descriptor and status flags.
		left, right := before[descriptor], after[descriptor]
		if inspect[descriptor] != nil || left.Dev != right.Dev || left.Ino != right.Ino || left.Rdev != right.Rdev || left.Mode != right.Mode ||
			beforeFlags[descriptor] != afterFlags[descriptor] || beforeStatus[descriptor] != afterStatus[descriptor] {
			t.Errorf("cleanup changed borrowed descriptor %d: %v", descriptor, inspect[descriptor])
		}
	}
	if accepted {
		t.Error("constructor admitted a malformed native acquisition")
	}
	if !errors.Is(closeErr, os.ErrInvalid) {
		t.Errorf("invalid native sink was not refused: %v", closeErr)
	}
	if os.Getenv(progressSinkOwnershipOperation) == "construct" && scenario == "unrepresentable-target" && !errors.Is(closeErr, errProgressDescriptorRange) {
		t.Errorf("constructor lost the descriptor range diagnosis: %v", closeErr)
	}
	if errors.Is(closeErr, unix.EBADF) {
		t.Error("cleanup closed a duplicate descriptor twice")
	}
	if scenario != "zero" {
		if !errors.Is(ownedErr, unix.EBADF) {
			t.Errorf("cleanup retained the owned descriptor: %v", ownedErr)
		}
	}
	if _, err := fmt.Fprintln(control, "operation"); err != nil {
		t.Fatal(err)
	}
}
