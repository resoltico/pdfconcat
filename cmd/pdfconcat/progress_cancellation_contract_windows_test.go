// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func TestProgressCancellationNativeResultContract(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		cancelled    uintptr
		native, want error
	}{
		{"success ignores stale access denied", 1, windows.ERROR_ACCESS_DENIED, nil},
		{"success ignores stale not found", 1, windows.ERROR_NOT_FOUND, nil},
		{"any nonzero BOOL succeeds", 2, windows.ERROR_INVALID_HANDLE, nil},
		{"idle completion race", 0, windows.ERROR_NOT_FOUND, nil},
		{"failure retains access denied", 0, windows.ERROR_ACCESS_DENIED, windows.ERROR_ACCESS_DENIED},
		{"failure retains invalid handle", 0, windows.ERROR_INVALID_HANDLE, windows.ERROR_INVALID_HANDLE},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			got := progressCancellationFailure(nil, scenario.cancelled, scenario.native)
			if scenario.want == nil && got != nil || scenario.want != nil && !errors.Is(got, scenario.want) {
				t.Fatalf("native cancellation result: %v, want %v", got, scenario.want)
			}
			previous := errors.New("earlier native cancellation failure")
			got = progressCancellationFailure(previous, scenario.cancelled, scenario.native)
			want := scenario.want
			if want == nil {
				want = previous
			}
			if !errors.Is(got, want) {
				t.Fatalf("cancellation lost the latest native failure: %v, want %v", got, want)
			}
		})
	}
}

func TestProgressCancellationUsesActualThreadRightsAndIdleResult(t *testing.T) {
	t.Parallel()
	procedure := windows.NewLazySystemDLL(progressWindowsKernel).NewProc("CancelSynchronousIo")
	requireProgressNoError(t, procedure.Find())
	for _, scenario := range []struct {
		name         string
		access       uint32
		native, want error
	}{
		{"query rights cannot cancel", windows.THREAD_QUERY_INFORMATION, windows.ERROR_ACCESS_DENIED, windows.ERROR_ACCESS_DENIED},
		{"idle authorized thread has no request", windows.THREAD_TERMINATE, windows.ERROR_NOT_FOUND, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			thread, err := windows.OpenThread(scenario.access, false, windows.GetCurrentThreadId())
			requireProgressNoError(t, err)
			defer func() { requireProgressNoError(t, windows.CloseHandle(thread)) }()
			cancelled, _, nativeErr := procedure.Call(uintptr(thread))
			if cancelled != 0 || !errors.Is(nativeErr, scenario.native) {
				t.Fatalf("actual native thread control returned BOOL=%d error=%v, want %v", cancelled, nativeErr, scenario.native)
			}
			got := progressCancellationFailure(nil, cancelled, nativeErr)
			if scenario.want == nil && got != nil || scenario.want != nil && !errors.Is(got, scenario.want) {
				t.Fatalf("actual native failure lost: %v, want %v", got, scenario.want)
			}
		})
	}
}
