// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package main

import (
	"errors"
	"io"
	"testing"

	"golang.org/x/sys/windows"
)

func TestProgressWindowsPipeQualificationPreservesNativeBackendFailure(t *testing.T) {
	if runProgressTransportChild(t, progressTransportChildBudget) {
		return
	}
	t.Parallel()
	reader, caller := progressPipe(t)
	handle := progressProducerHandle(t, caller)
	library := windows.NewLazySystemDLL("ntdll.dll")
	healthy := library.NewProc("NtQueryVolumeInformationFile")
	requireProgressNoError(t, healthy.Find())
	requireProgressNoError(t, qualifyProgressPipe(handle, healthy))

	// Same native signature, but information class 4 now requests
	// FILE_BASIC_INFORMATION, which cannot fit the eight-byte device buffer.
	// This is an actual completed native query failure on the healthy pipe.
	incompatible := library.NewProc("NtQueryInformationFile")
	requireProgressNoError(t, incompatible.Find())
	_, _, backendErr := progressPipeBackend(handle, incompatible)
	status, found := errors.AsType[windows.NTStatus](backendErr)
	if !found || status == windows.STATUS_SUCCESS || !errors.Is(backendErr, errProgressUnsupportedHandle) {
		t.Fatalf("native failure control did not establish a failed backend query: %v", backendErr)
	}
	qualificationErr := qualifyProgressPipe(handle, incompatible)
	if !errors.Is(qualificationErr, status) || !errors.Is(qualificationErr, errProgressUnsupportedHandle) {
		t.Fatalf("outer pipe qualification lost actual native backend cause %v: %v", status, qualificationErr)
	}

	_, err := caller.WriteString(progressProducerSentinel)
	requireProgressNoError(t, err)
	requireProgressNoError(t, caller.Close())
	data, err := io.ReadAll(reader)
	requireProgressNoError(t, err)
	if string(data) != progressProducerSentinel {
		t.Fatalf("failed qualification changed original pipe delivery: %q", data)
	}
}
