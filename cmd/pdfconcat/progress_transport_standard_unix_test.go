// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

const progressStandardDescriptorScenario = "PDFCONCAT_CLOSED_STANDARD_DESCRIPTOR"

func TestProgressTransportPreservesClosedStandardDescriptors(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	for _, descriptor := range []int{unix.Stdin, unix.Stdout} {
		t.Run(strconv.Itoa(descriptor), func(t *testing.T) {
			t.Parallel()
			command := exec.CommandContext(
				t.Context(),
				executable,
				progressHelperArgs("-test.run=^TestProgressClosedStandardDescriptorHelper$")...)

			command.Env = append(os.Environ(), progressStandardDescriptorScenario+"="+strconv.Itoa(descriptor))

			output, runErr := command.CombinedOutput()
			if runErr != nil {
				t.Fatalf("isolated closed-standard-descriptor boundary: %v\n%s", runErr, output)
			}
		})
	}
}

func TestProgressClosedStandardDescriptorHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressStandardDescriptorScenario)
	if scenario == "" {
		return
	}

	descriptor, err := strconv.Atoi(scenario)
	requireProgressNoError(t, err)

	if descriptor != unix.Stdin && descriptor != unix.Stdout {
		t.Fatal("unknown standard descriptor fixture")
	}

	_, writer := progressPipe(t)
	// Restore before Go's final test output; only this private subprocess changes descriptors.
	restore, err := unix.FcntlInt(uintptr(descriptor), unix.F_DUPFD_CLOEXEC, progressMinimumOwnedDescriptor)
	requireProgressNoError(t, err)
	t.Cleanup(func() {
		requireProgressNoError(t, unix.Dup2(restore, descriptor))
		requireProgressNoError(t, unix.Close(restore))
	})
	requireProgressNoError(t, unix.Close(descriptor))

	transport := progressNativeTransport(t, writer)
	if transport.fd < progressMinimumOwnedDescriptor || (transport.guard >= 0 && transport.guard < progressMinimumOwnedDescriptor) {
		t.Fatal("owned progress resource occupied a standard descriptor")
	}

	var stat unix.Stat_t
	if err = unix.Fstat(descriptor, &stat); !errors.Is(err, unix.EBADF) {
		t.Fatalf("closed caller standard descriptor became usable: %v", err)
	}

	if descriptor == unix.Stdout {
		if _, err = unix.Write(descriptor, []byte("final response")); !errors.Is(err, unix.EBADF) {
			t.Fatalf("final stdout failure hidden by progress ownership: %v", err)
		}
	}

	requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressEmptyRecord)))
	requireProgressNoError(t, transport.Close())
}

func TestProgressDescriptorProtectionRejectsInvalidNativeDescriptor(t *testing.T) {
	t.Parallel()

	descriptor, err := protectProgressDescriptor(-1)
	if descriptor != -1 || !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid descriptor protection: descriptor=%d error=%v", descriptor, err)
	}
}
