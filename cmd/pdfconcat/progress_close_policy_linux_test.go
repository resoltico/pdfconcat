// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type progressCloseDescriptorState struct {
	device uint64
	inode  uint64
	mode   uint32
	status int
	flags  int
}

const (
	progressClosePolicyScenario   = "PDFCONCAT_NATIVE_CLOSE_POLICY"
	progressClosePolicySelector   = "-test.run=^TestProgressClosePolicyHelper$"
	progressClosePolicyReceipt    = "native close policy verified"
	progressCloseDescriptorLimit  = 64
	progressCloseInheritedMinimum = 142
)

func TestProgressConstructorPreservesNativeRelocationCloseFailure(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs(progressClosePolicySelector, progressVerboseHelperArgument)...)

	command.Env = append(os.Environ(), progressClosePolicyScenario+"=close-zero")

	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), progressClosePolicyReceipt) ||
		!strings.Contains(string(output), "--- PASS: TestProgressClosePolicyHelper") {
		t.Fatalf("actual close-policy helper did not complete: %v\n%s", err, output)
	}

	t.Logf("actual private helper receipt:\n%s", output)
}

func TestProgressClosePolicyHelper(t *testing.T) {
	t.Parallel()

	if !progressClosePolicySelected(t) {
		return
	}
	// This dedicated child's irreversible thread policy never returns its M to the runtime pool.
	runtime.LockOSThread()

	tid := recordProgressClosePolicyEnvironment(t)
	reader, source := progressPipe(t)
	descriptor, err := nativeProgressDescriptor(source)
	requireProgressNoError(t, err)

	inheritedFile := newProgressCloseInheritedFile(t, descriptor)
	defer cleanupProgressCloseProbe(t, &inheritedFile)

	inventory := newProgressCloseInventory(t)
	defer cleanupProgressCloseProbe(t, &inventory)

	boundProgressCloseDescriptorTable(t)
	requireProgressNoError(t, unix.Close(unix.Stdin))

	assertProgressCloseZeroVacant(t)
	defer cleanupProgressCloseZero(t, descriptor)

	warmProgressCloseConstructor(t, reader, source)

	probeFile := newProgressCloseProbe(t, descriptor)
	defer cleanupProgressCloseProbe(t, &probeFile)

	installProgressClosePolicy(t)

	if err = unix.Close(unix.Stdin); !errors.Is(err, unix.EPERM) {
		t.Fatalf("vacant close(0) did not prove actual policy denial: %v", err)
	}

	closeErr := probeFile.Close()
	probeFile = nil

	requireProgressNoError(t, closeErr)
	before := snapshotProgressCloseDescriptors(t, inventory)

	transport, err := newProgressTransport(source)
	if transport != nil {
		defer cleanupProgressCloseTransport(t, transport)
	}

	if transport != nil || !errors.Is(err, unix.EPERM) || !strings.Contains(err.Error(), "release standard progress descriptor") {
		t.Fatalf("actual constructor lost successful-relocation/native-close failure: %v %v", transport, err)
	}

	if unix.Gettid() != tid {
		t.Fatal("constructor left its reviewed policy thread")
	}

	verifyProgressClosePreservation(t, inventory, inheritedFile, reader, source, before, int(descriptor))

	requireProgressNoError(t, source.Close())
	requireProgressNoError(t, reader.Close())
	t.Log(progressClosePolicyReceipt)
}

func progressClosePolicySelected(t *testing.T) bool {
	t.Helper()

	scenario := os.Getenv(progressClosePolicyScenario)
	if scenario == "" {
		return false
	}

	if scenario != "close-zero" {
		t.Fatal("unexpected close-policy scenario")
	}

	return true
}

func verifyProgressClosePreservation(
	t *testing.T,
	inventory, inherited, reader, source *os.File,
	before map[int]progressCloseDescriptorState,
	caller int,
) {
	t.Helper()
	requireProgressCloseInheritedSnapshot(t, inherited, before)

	after := snapshotProgressCloseDescriptors(t, inventory)
	assertProgressCloseOwnedState(t, before, after, caller)
	verifyProgressCloseCallerBytes(t, reader, source)
	verifyProgressCloseCallerBytes(t, reader, inherited)
	restoreProgressCloseDescriptors(t, inventory, before)
}

func newProgressCloseInheritedFile(t *testing.T, source uintptr) *os.File {
	t.Helper()

	descriptor, err := unix.FcntlInt(source, unix.F_DUPFD_CLOEXEC, progressCloseInheritedMinimum)
	requireProgressNoError(t, err)

	if descriptor < progressCloseInheritedMinimum {
		t.Fatal("actual high inherited descriptor was not installed")
	}

	return os.NewFile(uintptr(descriptor), "private-high-inherited-progress")
}

func newProgressCloseInventory(t *testing.T) *os.File {
	t.Helper()

	inventory, err := os.Open("/proc/self/fd")
	requireProgressNoError(t, err)

	return inventory
}

func requireProgressCloseInheritedSnapshot(t *testing.T, inherited *os.File, before map[int]progressCloseDescriptorState) {
	t.Helper()

	descriptor, err := nativeProgressDescriptor(inherited)
	requireProgressNoError(t, err)

	if _, ok := before[int(descriptor)]; !ok {
		t.Fatal("complete descriptor snapshot omitted the actual inherited descriptor")
	}
}

func boundProgressCloseDescriptorTable(t *testing.T) {
	t.Helper()

	requireProgressNoError(
		t,
		unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: progressCloseDescriptorLimit, Max: progressCloseDescriptorLimit}),
	)

	var actual unix.Rlimit
	requireProgressNoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &actual))

	if actual.Cur != progressCloseDescriptorLimit || actual.Max != progressCloseDescriptorLimit {
		t.Fatalf("descriptor ceiling was not installed: %+v", actual)
	}
}

func snapshotProgressCloseDescriptors(t *testing.T, inventory *os.File) map[int]progressCloseDescriptorState {
	t.Helper()

	result := map[int]progressCloseDescriptorState{}

	// RLIMIT bounds new allocations, but Linux retains descriptors inherited above it.
	// Reuse the directory opened before the zero-slot denial; opening here would claim fd zero.
	_, seekErr := inventory.Seek(0, io.SeekStart)
	requireProgressNoError(t, seekErr)

	entries, readErr := inventory.ReadDir(-1)
	requireProgressNoError(t, readErr)

	descriptors := make(map[int]struct{}, len(entries)+progressCloseDescriptorLimit)
	for descriptor := range progressCloseDescriptorLimit {
		descriptors[descriptor] = struct{}{}
	}

	for _, entry := range entries {
		descriptor, parseErr := strconv.Atoi(entry.Name())
		requireProgressNoError(t, parseErr)

		descriptors[descriptor] = struct{}{}
	}

	for descriptor := range descriptors {
		var stat unix.Stat_t

		statErr := unix.Fstat(descriptor, &stat)
		if errors.Is(statErr, unix.EBADF) {
			continue
		}

		requireProgressNoError(t, statErr)

		flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		requireProgressNoError(t, err)
		status, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
		requireProgressNoError(t, err)

		result[descriptor] = progressCloseDescriptorState{device: stat.Dev, inode: stat.Ino, mode: stat.Mode, status: status, flags: flags}
	}

	return result
}

func assertProgressCloseOwnedState(t *testing.T, before, after map[int]progressCloseDescriptorState, caller int) {
	t.Helper()

	owned, ok := after[unix.Stdin]
	if !ok || owned.mode&unix.S_IFMT != unix.S_IFIFO || owned.device != before[caller].device || owned.inode != before[caller].inode ||
		owned.status&unix.O_NONBLOCK == 0 ||
		owned.status&unix.O_ACCMODE != unix.O_WRONLY ||
		owned.flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("denied close did not retain the actual owned pipe at descriptor zero")
	}

	delete(after, unix.Stdin)

	if !equalProgressCloseDescriptors(before, after) {
		t.Fatal("constructor leaked its protected duplicate or changed a caller/runtime descriptor")
	}
}

func equalProgressCloseDescriptors(left, right map[int]progressCloseDescriptorState) bool {
	if len(left) != len(right) {
		return false
	}

	for descriptor, state := range left {
		if value, ok := right[descriptor]; !ok || state != value {
			return false
		}
	}

	return true
}

func assertProgressCloseZeroVacant(t *testing.T) {
	t.Helper()

	var stat unix.Stat_t
	if err := unix.Fstat(unix.Stdin, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("standard slot zero is not actually vacant: %v", err)
	}
}

func warmProgressCloseConstructor(t *testing.T, reader, source *os.File) {
	t.Helper()

	transport, err := newProgressTransport(source)
	if transport != nil {
		defer cleanupProgressCloseTransport(t, transport)
	}

	requireProgressNoError(t, err)

	if transport.fd < progressMinimumOwnedDescriptor {
		t.Fatal("positive constructor retained a standard slot")
	}

	assertProgressCloseZeroVacant(t)
	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressEmptyRecord)))
	content := make([]byte, len(progressEmptyRecord))
	_, err = io.ReadFull(reader, content)
	requireProgressNoError(t, err)

	if string(content) != progressEmptyRecord {
		t.Fatal("positive constructor changed complete bytes")
	}

	requireProgressNoError(t, transport.Close())
}

func verifyProgressCloseCallerBytes(t *testing.T, reader, source *os.File) {
	t.Helper()

	_, err := source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)

	content := make([]byte, len(progressEmptyRecord))
	_, err = io.ReadFull(reader, content)
	requireProgressNoError(t, err)

	if string(content) != progressEmptyRecord {
		t.Fatal("native close denial changed caller pipe usability")
	}
}

func cleanupProgressCloseTransport(t *testing.T, transport *progressTransport) {
	t.Helper()

	if err := transport.Close(); err != nil {
		t.Errorf("close owned policy transport: %v", err)
	}
}

func cleanupProgressCloseZero(t *testing.T, source uintptr) {
	t.Helper()

	var owned, caller unix.Stat_t
	if err := unix.Fstat(unix.Stdin, &owned); errors.Is(err, unix.EBADF) {
		return
	} else if err != nil {
		t.Errorf("inspect owned zero-slot cleanup: %v", err)
		return
	}

	if err := unix.Fstat(int(source), &caller); err != nil {
		t.Errorf("inspect caller identity before zero-slot cleanup: %v", err)
		return
	}

	if owned.Dev != caller.Dev || owned.Ino != caller.Ino || owned.Mode&unix.S_IFMT != unix.S_IFIFO {
		t.Error("zero-slot cleanup refused a different descriptor identity")
		return
	}

	if err := unix.CloseRange(0, 0, 0); err != nil {
		t.Errorf("release owned zero slot: %v", err)
		return
	}

	if err := unix.Fstat(unix.Stdin, &owned); !errors.Is(err, unix.EBADF) {
		t.Errorf("owned zero slot remains after cleanup: %v", err)
	}
}

func newProgressCloseProbe(t *testing.T, source uintptr) *os.File {
	t.Helper()

	descriptor, err := unix.FcntlInt(source, unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)
	requireProgressNoError(t, err)

	return os.NewFile(uintptr(descriptor), "private-close-policy-probe")
}

func cleanupProgressCloseProbe(t *testing.T, file **os.File) {
	t.Helper()

	if *file != nil {
		if err := (*file).Close(); err != nil {
			t.Errorf("close owned policy probe: %v", err)
		}

		*file = nil
	}
}

func restoreProgressCloseDescriptors(t *testing.T, inventory *os.File, before map[int]progressCloseDescriptorState) {
	t.Helper()
	requireProgressNoError(t, unix.CloseRange(0, 0, 0))
	assertProgressCloseZeroVacant(t)

	if current := snapshotProgressCloseDescriptors(t, inventory); !equalProgressCloseDescriptors(before, current) {
		t.Fatal("allowed close_range did not restore the complete preconstructor descriptor state")
	}
}
