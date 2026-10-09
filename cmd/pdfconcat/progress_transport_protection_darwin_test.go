// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type progressProtectionDescriptors struct {
	sink           progressNativeSink
	targetOwned    bool
	targetIdentity unix.Stat_t
	guard          progressPhaseFile
}

const (
	progressProtectionScenario = "PDFCONCAT_NATIVE_CANCELLATION_PROTECTION"
	progressProtectionSelector = "-test.run=^TestProgressNativeProtectionHelper$"
	progressProtectionReceipt  = "native cancellation descriptor finalization refusal verified"
)

var (
	errProgressProtectionReplacement = errors.New("actual Dup2 replacement and cleared CLOEXEC were not established")
	errProgressProtectionVacancy     = errors.New("retired cancellation descriptor was reused")
)

func TestProgressNativeCancellationProtectionFinalizationFailure(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs(progressProtectionSelector, progressVerboseHelperArgument)...)

	command.Env = append(os.Environ(), progressProtectionScenario+"=1")
	assertProgressNativeChild(ctx, t, command, progressProtectionReceipt, "TestProgressNativeProtectionHelper")
}

func TestProgressNativeProtectionHelper(t *testing.T) {
	if os.Getenv(progressProtectionScenario) == "" {
		return
	}

	if os.Getenv(progressProtectionScenario) != "1" {
		t.Fatal("unknown cancellation protection scenario")
	}

	reader, source := progressPipe(t)
	_, err := source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)
	assertProgressPhaseBytes(t, reader, progressEmptyRecord)
	caller := captureProgressPhaseFile(t, source)
	verifyHealthyProgressProtection(t, reader, source)

	raw := acquireProgressProtectionDescriptors(t, source)
	defer raw.release(t)

	protectionErr := retireProgressProtectionTarget(raw)
	if !errors.Is(protectionErr, unix.EBADF) || !strings.Contains(protectionErr.Error(), "protect progress cancellation descriptor") {
		t.Fatalf("actual retired F_SETFD target was not refused: %v", protectionErr)
	}

	requireRelocationVacancy(t, raw.sink.fd)
	assertProgressPhaseFile(t, source, caller)
	_, err = source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)
	assertProgressPhaseBytes(t, reader, progressEmptyRecord)
	assertProgressPhaseEmpty(t, reader)
	assertProgressProtectionGuard(t, raw)
	raw.release(t)
	requireRelocationVacancy(t, raw.sink.guard)
	t.Log(progressProtectionReceipt)
}

func verifyHealthyProgressProtection(t *testing.T, reader, source *os.File) {
	t.Helper()

	transport, err := newProgressTransport(source)

	requireProgressNoError(t, err)
	defer func() { requireProgressNoError(t, transport.Close()) }()

	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressTestRecord)))
	assertProgressPhaseBytes(t, reader, progressTestRecord)
	requireProgressNoError(t, interruptProgressPipe(transport.fd, transport.guard))
	requireProgressNoError(t, transport.Close())
	requireRelocationVacancy(t, transport.fd)
	requireRelocationVacancy(t, transport.guard)
}

func acquireProgressProtectionDescriptors(t *testing.T, source *os.File) *progressProtectionDescriptors {
	t.Helper()
	caller := captureProgressPhaseFile(t, source)

	var expectedGuard unix.Stat_t
	requireProgressNoError(t, unix.Stat("/dev/null", &expectedGuard))

	sink, err := openProgressDescriptor(source)
	requireProgressNoError(t, err)

	raw := &progressProtectionDescriptors{
		sink: sink, targetOwned: true, targetIdentity: caller.identity,
		guard: progressPhaseFile{descriptor: uintptr(sink.guard), identity: expectedGuard},
	}

	handed := false
	defer func() {
		if !handed {
			raw.release(t)
		}
	}()

	var guard unix.Stat_t
	requireProgressNoError(t, unix.Fstat(sink.guard, &guard))

	if guard.Dev != expectedGuard.Dev || guard.Ino != expectedGuard.Ino ||
		guard.Rdev != expectedGuard.Rdev || guard.Mode != expectedGuard.Mode {
		t.Fatal("acquired guard does not identify the original null device")
	}

	status, err := unix.FcntlInt(uintptr(sink.guard), unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	flags, err := unix.FcntlInt(uintptr(sink.guard), unix.F_GETFD, 0)
	requireProgressNoError(t, err)

	raw.guard = progressPhaseFile{descriptor: uintptr(sink.guard), identity: guard, status: status, flags: flags}
	if guard.Mode&unix.S_IFMT != unix.S_IFCHR || status&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("healthy cancellation guard has unexpected native state")
	}

	handed = true

	return raw
}

func retireProgressProtectionTarget(raw *progressProtectionDescriptors) error {
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()

	if err := unix.Dup2(raw.sink.guard, raw.sink.fd); err != nil {
		return fmt.Errorf("replace cancellation target: %w", err)
	}

	raw.targetIdentity = raw.guard.identity
	if err := inspectProgressProtectionReplacement(raw.sink.fd, raw.guard); err != nil {
		return err
	}

	var vacant unix.Stat_t
	// No fixture opens, logs or setup wrappers intervene between retirement and the production finalizer.
	if err := unix.Close(raw.sink.fd); err != nil {
		return fmt.Errorf("retire cancellation target: %w", err)
	}

	raw.targetOwned = false
	if err := unix.Fstat(raw.sink.fd, &vacant); !errors.Is(err, unix.EBADF) {
		return errors.Join(errProgressProtectionVacancy, err)
	}

	return protectCancellationDescriptor(raw.sink.fd)
}

func inspectProgressProtectionReplacement(descriptor int, guard progressPhaseFile) error {
	var replaced unix.Stat_t
	if err := unix.Fstat(descriptor, &replaced); err != nil {
		return fmt.Errorf("inspect cancellation replacement: %w", err)
	}

	status, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	if err != nil {
		return fmt.Errorf("inspect cancellation replacement status: %w", err)
	}

	flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
	if err != nil {
		return fmt.Errorf("inspect cancellation replacement flags: %w", err)
	}

	if replaced.Dev != guard.identity.Dev || replaced.Ino != guard.identity.Ino || replaced.Rdev != guard.identity.Rdev ||
		replaced.Mode&unix.S_IFMT != unix.S_IFCHR || status != guard.status || flags&unix.FD_CLOEXEC != 0 {
		return errProgressProtectionReplacement
	}

	return nil
}

func assertProgressProtectionGuard(t *testing.T, raw *progressProtectionDescriptors) {
	t.Helper()

	var identity unix.Stat_t
	requireProgressNoError(t, unix.Fstat(raw.sink.guard, &identity))
	status, err := unix.FcntlInt(uintptr(raw.sink.guard), unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	flags, err := unix.FcntlInt(uintptr(raw.sink.guard), unix.F_GETFD, 0)
	requireProgressNoError(t, err)

	before := raw.guard
	if identity.Dev != before.identity.Dev || identity.Ino != before.identity.Ino || identity.Rdev != before.identity.Rdev ||
		identity.Mode&unix.S_IFMT != before.identity.Mode&unix.S_IFMT || status != before.status || flags != before.flags {
		t.Fatal("finalization changed cancellation guard identity or complete flags")
	}
}

func (raw *progressProtectionDescriptors) release(t *testing.T) {
	t.Helper()

	if raw.targetOwned {
		closeProgressProtectionIdentity(t, raw.sink.fd, raw.targetIdentity)
		raw.targetOwned = false
	}

	if raw.guard.descriptor != 0 {
		closeProgressProtectionIdentity(t, raw.sink.guard, raw.guard.identity)
		raw.guard.descriptor = 0
	}
}

func closeProgressProtectionIdentity(t *testing.T, descriptor int, expected unix.Stat_t) {
	t.Helper()

	var actual unix.Stat_t
	if err := unix.Fstat(descriptor, &actual); err != nil {
		t.Error(err)
		return
	}

	if actual.Dev != expected.Dev || actual.Ino != expected.Ino || actual.Rdev != expected.Rdev || actual.Mode != expected.Mode {
		t.Error("cleanup refused a reused cancellation descriptor")
		return
	}

	if err := unix.Close(descriptor); err != nil {
		t.Error(err)
	}
}
