// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package main

import (
	"bytes"
	"errors"
	"maps"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func assertProgressOwnedWaitFailure(t *testing.T) {
	t.Helper()
	// The irreversible filter belongs to this private child and its locked M only.
	runtime.LockOSThread()

	tid := recordProgressClosePolicyEnvironment(t)

	fixture := &progressZeroFixture{}
	defer fixture.retire(t)

	fixture.prepare(t)
	fixture.warm(t)
	prefix := fillProgressOwnedWait(t, fixture.transport.fd)
	before := snapshotProgressLinuxFiles(t)
	installProgressWaitPolicy(t, fixture.transport.pollDescriptor)

	t.Logf("owned wait policy tid=%d native_ppoll=%d nfds=1", tid, unix.SYS_PPOLL)

	err := fixture.transport.WriteRecord(t.Context(), []byte(progressEmptyRecord))
	if !errors.Is(err, unix.EPERM) || !strings.Contains(err.Error(), "poll progress sink") || t.Context().Err() != nil {
		t.Fatalf("actual owned backpressure did not reach ppoll EPERM: %v", err)
	}

	if !fixture.transport.Interrupted() || unix.Gettid() != tid {
		t.Fatal("actual wait failure did not poison its channel on the policy thread")
	}

	assertProgressPoisoned(t.Context(), t, fixture.transport)
	requireProgressNoError(t, fixture.transport.Close())

	var stat unix.Stat_t
	if err = unix.Fstat(fixture.transport.fd, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("owned failed-wait descriptor not released: %v", err)
	}

	delete(before, fixture.transport.fd)

	if !maps.Equal(before, snapshotProgressLinuxFiles(t)) {
		t.Fatal("failed native wait changed caller or standard descriptor state")
	}

	fixture.startReader() // Only now may the independent consumer remove backpressure.
	_, err = fixture.source.WriteString(progressZeroSentinel)
	requireProgressNoError(t, err)
	result := fixture.retire(t)

	prefix = append(prefix, []byte(progressZeroSentinel)...)
	if result.err != nil || !bytes.Equal(result.content, prefix) {
		t.Fatalf("wait failure appended progress or changed caller bytes: %v", result.err)
	}
}

func fillProgressOwnedWait(t *testing.T, descriptor int) []byte {
	t.Helper()

	var prefix []byte

	for _, buffer := range [][]byte{make([]byte, 4096), {0}} {
		full := false

		for range 65536 {
			count, err := unix.Write(descriptor, buffer)
			if errors.Is(err, unix.EAGAIN) {
				full = true
				break
			}

			requireProgressNoError(t, err)

			if count <= 0 || count > len(buffer) {
				t.Fatal("owned wait fixture did not make native write progress")
			}

			prefix = append(prefix, buffer[:count]...)
		}

		if !full {
			t.Fatal("owned pipe did not reach actual single-byte EAGAIN")
		}
	}

	return prefix
}

// The policy denies ppoll's argument-count class, not a dereferenced FD.
// The wrapped error and prefilled owned-flow witness establish attribution.
func installProgressWaitPolicy(t *testing.T, descriptor int32) {
	t.Helper()

	poll := []unix.PollFd{{Fd: descriptor, Events: unix.POLLOUT}}
	count, pollErr := unix.Poll(poll, 0)
	requireProgressNoError(t, pollErr)

	if count != 0 {
		t.Fatal("healthy native poll did not observe the genuinely full pipe")
	}

	const argumentCountOffset = 24 // seccomp_data.args[1], on the supported little-endian LP64 ABIs.

	filters := [9]unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: progressCloseAuditArchitecture(t), Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_PPOLL, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: argumentCountOffset},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 1, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	installProgressNativeFilter(t, filters)

	count, pollErr = unix.Poll(nil, 0)
	requireProgressNoError(t, pollErr)

	if count != 0 {
		t.Fatal("private wait policy changed zero-descriptor polling")
	}
}
