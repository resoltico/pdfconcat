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
	progressClosePolicyScenario  = "PDFCONCAT_NATIVE_CLOSE_POLICY"
	progressClosePolicySelector  = "-test.run=^TestProgressClosePolicyHelper$"
	progressClosePolicyReceipt   = "native close policy verified"
	progressCloseDescriptorLimit = 64
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

	scenario := os.Getenv(progressClosePolicyScenario)
	if scenario == "" {
		return
	}

	if scenario != "close-zero" {
		t.Fatal("unexpected close-policy scenario")
	}
	// This dedicated child's irreversible thread policy never returns its M to the runtime pool.
	runtime.LockOSThread()

	tid := recordProgressClosePolicyEnvironment(t)
	reader, source := progressPipe(t)
	descriptor, err := nativeProgressDescriptor(source)
	requireProgressNoError(t, err)
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
	before := snapshotProgressCloseDescriptors(t)

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

	after := snapshotProgressCloseDescriptors(t)
	assertProgressCloseOwnedState(t, before, after, int(descriptor))
	verifyProgressCloseCallerBytes(t, reader, source)
	restoreProgressCloseDescriptors(t, before)

	requireProgressNoError(t, source.Close())
	requireProgressNoError(t, reader.Close())
	t.Log(progressClosePolicyReceipt)
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

	entries, err := os.ReadDir("/proc/self/fd")
	requireProgressNoError(t, err)

	for _, entry := range entries {
		descriptor, parseErr := strconv.Atoi(entry.Name())
		requireProgressNoError(t, parseErr)

		if descriptor >= progressCloseDescriptorLimit {
			var stat unix.Stat_t
			if statErr := unix.Fstat(descriptor, &stat); !errors.Is(statErr, unix.EBADF) {
				t.Fatalf("inherited descriptor outside complete bounded scan: %d %v", descriptor, statErr)
			}
		}
	}
}

func snapshotProgressCloseDescriptors(t *testing.T) map[int]progressCloseDescriptorState {
	t.Helper()

	result := map[int]progressCloseDescriptorState{}

	for descriptor := range progressCloseDescriptorLimit {
		var stat unix.Stat_t

		err := unix.Fstat(descriptor, &stat)
		if errors.Is(err, unix.EBADF) {
			continue
		}

		requireProgressNoError(t, err)
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

func restoreProgressCloseDescriptors(t *testing.T, before map[int]progressCloseDescriptorState) {
	t.Helper()
	requireProgressNoError(t, unix.CloseRange(0, 0, 0))
	assertProgressCloseZeroVacant(t)

	if current := snapshotProgressCloseDescriptors(t); !equalProgressCloseDescriptors(before, current) {
		t.Fatal("allowed close_range did not restore the complete preconstructor descriptor state")
	}
}
