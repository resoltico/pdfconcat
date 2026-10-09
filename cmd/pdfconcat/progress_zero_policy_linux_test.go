// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type (
	progressZeroRead struct {
		err     error
		content []byte
	}

	progressZeroFixture struct {
		transport   *progressTransport
		source      *os.File
		reader      *os.File
		readResult  chan progressZeroRead
		readStopped chan struct{}
		closed      bool
	}
)

const (
	progressZeroScenario = "PDFCONCAT_NATIVE_ZERO_WRITE"
	progressZeroReceipt  = "native zero write verified"
	progressZeroSelector = "-test.run=^TestProgressZeroWritePolicyHelper$"
	progressZeroSentinel = "original-zero-policy-caller\n"
)

func TestProgressRejectsNativeZeroWrite(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, executable, progressHelperArgs(progressZeroSelector, progressVerboseHelperArgument)...)

	command.Env = append(os.Environ(), progressZeroScenario+"=owned-write-zero")

	output, err := command.CombinedOutput()
	if err != nil || ctx.Err() != nil || !strings.Contains(string(output), progressZeroReceipt) ||
		!strings.Contains(string(output), "--- PASS: TestProgressZeroWritePolicyHelper") {
		t.Fatalf("actual zero-write helper did not finish: %v\n%s", err, output)
	}

	t.Logf("actual private zero-write receipt:\n%s", output)
}

func TestProgressZeroWritePolicyHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressZeroScenario)
	if scenario == "" {
		return
	}

	if scenario != "owned-write-zero" {
		t.Fatal("unexpected zero-write scenario")
	}

	runtime.LockOSThread()

	tid := recordProgressZeroEnvironment(t)

	fixture := &progressZeroFixture{}
	defer fixture.retire(t)

	fixture.prepare(t)
	owned := fixture.transport.fd
	caller, err := nativeProgressDescriptor(fixture.source)
	requireProgressNoError(t, err)

	if owned < progressMinimumOwnedDescriptor || uintptr(owned) == caller {
		t.Fatal("filter target is not independently owned")
	}

	before := snapshotProgressZeroCaller(t, int(caller))
	fixture.warm(t)
	fixture.startReader()
	installProgressZeroPolicy(t, owned)
	verifyProgressZeroCalls(t, fixture.transport, tid)
	written, err := fixture.source.WriteString(progressZeroSentinel)
	requireProgressNoError(t, err)

	if written != len(progressZeroSentinel) {
		t.Fatal("caller sentinel was filtered")
	}

	if before != snapshotProgressZeroCaller(t, int(caller)) {
		t.Fatal("zero policy changed caller flags/identity")
	}

	result := fixture.retire(t)

	var stat unix.Stat_t
	if err = unix.Fstat(owned, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("owned zero descriptor not closed: %v", err)
	}

	if result.err != nil || string(result.content) != progressZeroSentinel {
		t.Fatalf("native zero calls delivered bytes or reader failed: %q %v", result.content, result.err)
	}

	t.Logf("native zero policy completed tid=%d native_write=%d owned=%d caller=%d", tid, unix.SYS_WRITE, owned, caller)
	t.Log(progressZeroReceipt)
}

func (fixture *progressZeroFixture) prepare(t *testing.T) {
	t.Helper()

	var err error

	fixture.reader, fixture.source, err = os.Pipe()
	requireProgressNoError(t, err)
	fixture.transport, err = newProgressTransport(fixture.source)
	requireProgressNoError(t, err)

	if fixture.transport.interruptiblePipe {
		t.Fatal("Linux witness unexpectedly selected a worker")
	}
}

func (fixture *progressZeroFixture) warm(t *testing.T) {
	t.Helper()
	requireProgressNoError(t, fixture.transport.WriteRecord(t.Context(), []byte(progressEmptyRecord)))
	positive := make([]byte, len(progressEmptyRecord))
	_, err := io.ReadFull(fixture.reader, positive)
	requireProgressNoError(t, err)

	if string(positive) != progressEmptyRecord {
		t.Fatal("healthy prepolicy delivery changed bytes")
	}
}

func (fixture *progressZeroFixture) startReader() {
	fixture.readResult = make(chan progressZeroRead, 1)

	fixture.readStopped = make(chan struct{})
	go func() {
		defer close(fixture.readStopped)

		content, err := io.ReadAll(fixture.reader)
		fixture.readResult <- progressZeroRead{err: err, content: content}
	}()
}

func (fixture *progressZeroFixture) retire(t *testing.T) progressZeroRead {
	t.Helper()

	if fixture.closed {
		return progressZeroRead{}
	}

	fixture.closed = true
	if fixture.transport != nil {
		if err := fixture.transport.Close(); err != nil {
			t.Errorf("close zero-write transport: %v", err)
		}
	}

	if fixture.source != nil {
		if err := fixture.source.Close(); err != nil {
			t.Errorf("close zero-write caller: %v", err)
		}
	}

	result := fixture.joinReader(t)
	if fixture.reader != nil {
		if err := fixture.reader.Close(); err != nil {
			t.Errorf("close zero-write reader: %v", err)
		}
	}

	return result
}

func (fixture *progressZeroFixture) joinReader(t *testing.T) progressZeroRead {
	t.Helper()

	if fixture.readResult == nil {
		return progressZeroRead{}
	}

	select {
	case result := <-fixture.readResult:
		<-fixture.readStopped
		return result
	case <-time.After(time.Second):
		t.Error("zero-write reader was not joined; parent must retire failed child")

		for {
			time.Sleep(time.Second)
		}
	}
}

func verifyProgressZeroCalls(t *testing.T, transport *progressTransport, tid int) {
	t.Helper()

	if unix.Gettid() != tid {
		t.Fatal("zero-write policy left its native thread")
	}

	verifyNativeZeroChunk(t, transport.fd)

	err := transport.WriteRecord(t.Context(), []byte(progressEmptyRecord))
	if !errors.Is(err, io.ErrNoProgress) || !transport.Interrupted() {
		t.Fatalf("actual zero record falsely delivered: %v", err)
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("zero record masked context cancellation: %v", err)
	}

	if err = transport.WriteRecord(t.Context(), []byte(progressEmptyRecord)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("poisoned zero channel accepted followup: %v", err)
	}

	if unix.Gettid() != tid {
		t.Fatal("native/chunk/record control changed its installation thread")
	}
}

func installProgressZeroPolicy(t *testing.T, owned int) {
	t.Helper()
	architecture := progressCloseAuditArchitecture(t)
	filters := [9]unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: architecture, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_WRITE, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(owned), Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	installProgressNativeFilter(t, filters)
}

func snapshotProgressZeroCaller(t *testing.T, descriptor int) progressCloseDescriptorState {
	t.Helper()

	var stat unix.Stat_t
	requireProgressNoError(t, unix.Fstat(descriptor, &stat))
	flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
	requireProgressNoError(t, err)
	status, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	requireProgressNoError(t, err)

	return progressCloseDescriptorState{device: stat.Dev, inode: stat.Ino, mode: stat.Mode, status: status, flags: flags}
}

func recordProgressZeroEnvironment(t *testing.T) int {
	t.Helper()

	mode, err := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	requireProgressNoError(t, err)
	nnp, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	requireProgressNoError(t, err)

	var kernel unix.Utsname
	requireProgressNoError(t, unix.Uname(&kernel))

	groups, err := unix.Getgroups()
	requireProgressNoError(t, err)

	tid := unix.Gettid()
	t.Logf("private zero policy: go=%s arch=%s kernel=%s uid=%d gid=%d groups=%v tid=%d "+
		"inherited_seccomp=%d inherited_nnp=%d native_write=%d audit_arch=%#x", runtime.Version(), runtime.GOARCH,
		unix.ByteSliceToString(kernel.Release[:]), unix.Getuid(), unix.Getgid(), groups, tid, mode, nnp,
		unix.SYS_WRITE, progressCloseAuditArchitecture(t))

	return tid
}

func verifyNativeZeroChunk(t *testing.T, descriptor int) {
	t.Helper()

	record := []byte(progressEmptyRecord)
	original := bytes.Clone(record)

	count, err := unix.Write(descriptor, record)
	if count != 0 || err != nil {
		t.Fatalf("actual kernel control is not zero/nil: %d %v", count, err)
	}

	pending, err := writeProgressChunk(descriptor, record)
	if !errors.Is(err, io.ErrNoProgress) || !bytes.Equal(pending, original) || !bytes.Equal(record, original) {
		t.Fatalf("zero chunk advanced or lost pending bytes: %q %v", pending, err)
	}
}
