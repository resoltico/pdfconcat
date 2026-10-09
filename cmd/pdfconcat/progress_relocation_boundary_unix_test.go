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
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type progressRelocationCaller struct {
	reader          *os.File
	source          *os.File
	descriptor      uintptr
	statusFlags     int
	descriptorFlags int
	identity        unix.Stat_t
}

const (
	progressRelocationScenario = "PDFCONCAT_RELOCATION_BOUNDARY"
	progressRelocationSelector = "-test.run=^TestProgressRelocationBoundaryHelper$"
	progressRelocationReceipt  = "native relocation finalization verified"
)

func TestProgressRelocationFinalizationRejectsRetiredOriginal(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs(progressRelocationSelector, progressVerboseHelperArgument)...)

	command.Env = append(os.Environ(), progressRelocationScenario+"=retired-original")

	output, err := command.CombinedOutput()
	if err != nil || ctx.Err() != nil || !strings.Contains(string(output), progressRelocationReceipt) ||
		!strings.Contains(string(output), "--- PASS: TestProgressRelocationBoundaryHelper") {
		t.Fatalf("actual relocation helper did not finish: %v\n%s", err, output)
	}

	t.Logf("native finalization boundary receipt:\n%s", output)
}

func TestProgressRelocationBoundaryHelper(t *testing.T) {
	if os.Getenv(progressRelocationScenario) == "" {
		return
	}

	if os.Getenv(progressRelocationScenario) != "retired-original" {
		t.Fatal("unexpected relocation scenario")
	}

	caller := captureRelocationCaller(t)

	var err error
	if err = unix.Close(unix.Stdin); err != nil && !errors.Is(err, unix.EBADF) {
		t.Fatal(err)
	}

	requireRelocationVacancy(t, unix.Stdin)
	requireProgressNoError(t, unix.Dup2(int(caller.descriptor), unix.Stdin))

	originalLive := true
	protected := -1

	defer func() {
		if originalLive {
			cleanupRelocationPipe(t, unix.Stdin, int(caller.descriptor))
		}

		if protected >= 0 {
			cleanupRelocationPipe(t, protected, int(caller.descriptor))
		}
	}()

	protected, acquisitionErr := unix.FcntlInt(uintptr(unix.Stdin), unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)
	requireProgressNoError(t, acquisitionErr)
	assertProtectedRelocation(t, caller, protected)

	var vacantIdentity unix.Stat_t
	// All setup precedes retirement; ownership transfers before finalization.
	retirementErr := unix.Close(unix.Stdin)
	if retirementErr != nil {
		t.Fatal(retirementErr)
	}

	originalLive = false

	vacancyErr := unix.Fstat(unix.Stdin, &vacantIdentity)
	if !errors.Is(vacancyErr, unix.EBADF) {
		t.Fatalf("original descriptor reused before finalization: %v", vacancyErr)
	}

	handedDescriptor := protected
	protected = -1

	result, err := completeProgressRelocation(unix.Stdin, handedDescriptor, acquisitionErr)
	if result != -1 || !errors.Is(err, unix.EBADF) || !strings.Contains(err.Error(), "release standard progress descriptor") {
		t.Fatalf("retired acquisition not refused: %d %v", result, err)
	}

	requireRelocationVacancy(t, unix.Stdin)
	requireRelocationVacancy(t, handedDescriptor)
	assertRelocationCallerPreserved(t, caller)
	t.Log(progressRelocationReceipt)
}

func captureRelocationCaller(t *testing.T) *progressRelocationCaller {
	t.Helper()
	reader, source := progressPipe(t)
	descriptor, err := nativeProgressDescriptor(source)
	requireProgressNoError(t, err)
	readerDescriptor, err := nativeProgressDescriptor(reader)
	requireProgressNoError(t, err)

	if descriptor < progressMinimumOwnedDescriptor || readerDescriptor < progressMinimumOwnedDescriptor {
		t.Fatal("relocation fixture pipe occupied a standard descriptor before setup")
	}

	statusFlags, err := unix.FcntlInt(descriptor, unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	descriptorFlags, err := unix.FcntlInt(descriptor, unix.F_GETFD, 0)
	requireProgressNoError(t, err)

	caller := &progressRelocationCaller{
		reader: reader, source: source, descriptor: descriptor,
		statusFlags: statusFlags, descriptorFlags: descriptorFlags,
	}
	requireProgressNoError(t, unix.Fstat(int(descriptor), &caller.identity))

	if caller.identity.Mode&unix.S_IFMT != unix.S_IFIFO || caller.statusFlags&unix.O_ACCMODE != unix.O_WRONLY {
		t.Fatal("caller baseline is not an actual write-only pipe")
	}

	return caller
}

func assertProtectedRelocation(t *testing.T, caller *progressRelocationCaller, protected int) {
	t.Helper()

	var identity unix.Stat_t
	requireProgressNoError(t, unix.Fstat(protected, &identity))
	status, err := unix.FcntlInt(uintptr(protected), unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	flags, err := unix.FcntlInt(uintptr(protected), unix.F_GETFD, 0)
	requireProgressNoError(t, err)

	if identity.Dev != caller.identity.Dev || identity.Ino != caller.identity.Ino || identity.Mode&unix.S_IFMT != unix.S_IFIFO ||
		status != caller.statusFlags || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("protected acquisition did not preserve the caller pipe and flags")
	}
}

func assertRelocationCallerPreserved(t *testing.T, caller *progressRelocationCaller) {
	t.Helper()

	status, err := unix.FcntlInt(caller.descriptor, unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	flags, err := unix.FcntlInt(caller.descriptor, unix.F_GETFD, 0)
	requireProgressNoError(t, err)

	var identity unix.Stat_t
	requireProgressNoError(t, unix.Fstat(int(caller.descriptor), &identity))

	if caller.statusFlags != status || caller.descriptorFlags != flags || caller.identity.Dev != identity.Dev ||
		caller.identity.Ino != identity.Ino || identity.Mode&unix.S_IFMT != unix.S_IFIFO {
		t.Fatal("finalization changed caller status, descriptor flags or identity")
	}

	_, err = caller.source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)

	data := make([]byte, len(progressEmptyRecord))
	_, err = io.ReadFull(caller.reader, data)
	requireProgressNoError(t, err)

	if string(data) != progressEmptyRecord {
		t.Fatal("finalization changed caller bytes")
	}
}

func requireRelocationVacancy(t *testing.T, descriptor int) {
	t.Helper()

	var stat unix.Stat_t
	if err := unix.Fstat(descriptor, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("descriptor %d is not vacant: %v", descriptor, err)
	}
}

func cleanupRelocationPipe(t *testing.T, descriptor, caller int) {
	t.Helper()

	var owned, original unix.Stat_t
	if err := unix.Fstat(descriptor, &owned); errors.Is(err, unix.EBADF) {
		return
	} else if err != nil {
		t.Error(err)
		return
	}

	if err := unix.Fstat(caller, &original); err != nil {
		t.Error(err)
		return
	}

	if owned.Dev != original.Dev || owned.Ino != original.Ino || owned.Mode&unix.S_IFMT != unix.S_IFIFO {
		t.Error("cleanup refused a reused non-owned descriptor")
		return
	}

	if err := unix.Close(descriptor); err != nil {
		t.Error(err)
	}
}
