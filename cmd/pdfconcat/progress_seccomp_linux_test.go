// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package main

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func installProgressClosePolicy(t *testing.T) {
	t.Helper()
	architecture := progressCloseAuditArchitecture(t)
	filters := [9]unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: architecture, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_CLOSE, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 0, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	installProgressNativeFilter(t, filters)
}

func installProgressNativeFilter(t *testing.T, filters [9]unix.SockFilter) {
	t.Helper()

	program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}

	field, ok := reflect.TypeOf(program).FieldByName("Filter")
	if !ok || reflect.TypeOf(program).Size() != 16 || field.Offset != 8 || reflect.TypeOf(filters[0]).Size() != 8 {
		t.Fatal("seccomp filter does not match reviewed native LP64 ABI")
	}

	var pins runtime.Pinner
	pins.Pin(&program)

	pins.Pin(&filters[0])
	defer pins.Unpin()

	requireProgressNoError(t, unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0))
	// Both user allocations stay pinned until the synchronous kernel copy finishes.
	_, _, errno := unix.Syscall6(unix.SYS_PRCTL, unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0, 0)
	runtime.KeepAlive(&program)
	runtime.KeepAlive(&filters)
	runtime.KeepAlive(&pins)

	if errno != 0 {
		t.Fatalf("install private native policy: %v", errno)
	}

	mode, err := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	requireProgressNoError(t, err)
	nnp, err := unix.PrctlRetInt(unix.PR_GET_NO_NEW_PRIVS, 0, 0, 0, 0)
	requireProgressNoError(t, err)

	if mode != unix.SECCOMP_MODE_FILTER || nnp != 1 {
		t.Fatalf("installed policy facts mode=%d nnp=%d", mode, nnp)
	}

	t.Logf("installed private policy: tid=%d seccomp=%d nnp=%d instructions=%d", unix.Gettid(), mode, nnp, len(filters))
}

func progressCloseAuditArchitecture(t *testing.T) uint32 {
	t.Helper()

	switch runtime.GOARCH {
	case "amd64":
		return unix.AUDIT_ARCH_X86_64
	case "arm64":
		return unix.AUDIT_ARCH_AARCH64
	default:
		t.Fatal("close policy requires native Linux amd64 or arm64")
		return 0
	}
}

func recordProgressClosePolicyEnvironment(t *testing.T) int {
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
	t.Logf(
		"private native close policy: go=%s arch=%s kernel=%s uid=%d euid=%d gid=%d groups=%v tid=%d "+
			"inherited_seccomp=%d inherited_nnp=%d native_close=%d audit_arch=%#x",
		runtime.Version(),
		runtime.GOARCH,
		unix.ByteSliceToString(kernel.Release[:]),
		unix.Getuid(),
		unix.Geteuid(),
		unix.Getgid(),
		groups,
		tid,
		mode,
		nnp,
		unix.SYS_CLOSE,
		progressCloseAuditArchitecture(t),
	)

	return tid
}
