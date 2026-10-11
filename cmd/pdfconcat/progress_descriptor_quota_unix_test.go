// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProgressProtectedDescriptorSurvivesExhaustedQuota(t *testing.T) {
	if os.Getenv(progressTransportChild) != t.Name() {
		reader, writer := progressPipe(t)
		// ExtraFiles reserves owned descriptor 3 before the child runtime initializes.
		if runProgressTransportChild(t, progressTransportChildBudget, writer) {
			requireProgressNoError(t, writer.Close())
			data, err := io.ReadAll(reader)
			requireProgressNoError(t, err)
			if !bytes.Equal(data, []byte(progressTestRecord)) {
				t.Fatal("protected native pipe lost complete bytes")
			}
			return
		}
	}
	if runProgressTransportChild(t, progressTransportChildBudget) {
		return
	}
	descriptor := unix.Stderr + 1 // ExtraFiles[0] is descriptor 3 by the exec contract.
	t.Cleanup(func() {
		if err := unix.Close(descriptor); err != nil && !errors.Is(err, unix.EBADF) {
			t.Error(err)
		}
	})
	var before unix.Stat_t
	requireProgressNoError(t, unix.Fstat(descriptor, &before))
	flags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
	requireProgressNoError(t, err)
	_, err = unix.FcntlInt(uintptr(descriptor), unix.F_SETFD, flags|unix.FD_CLOEXEC)
	requireProgressNoError(t, err)
	status, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	if before.Mode&unix.S_IFMT != unix.S_IFIFO || status&unix.O_ACCMODE != unix.O_WRONLY {
		t.Fatal("inherited descriptor is not an actual write-only pipe")
	}
	var original unix.Rlimit
	requireProgressNoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &original))
	quota := original
	quota.Cur = 4 // No new slot exists, but descriptor 3 remains valid.
	requireProgressNoError(t, unix.Setrlimit(unix.RLIMIT_NOFILE, &quota))
	protected, protectErr := protectProgressDescriptor(descriptor)
	restoreErr := unix.Setrlimit(unix.RLIMIT_NOFILE, &original) // Restore before reporting or receipt I/O.
	requireProgressNoError(t, restoreErr)
	var restored unix.Rlimit
	requireProgressNoError(t, unix.Getrlimit(unix.RLIMIT_NOFILE, &restored))
	if restored != original {
		t.Fatal("descriptor quota was not restored before reporting")
	}
	if protectErr != nil || protected != descriptor {
		t.Fatalf("already protected sink required an unavailable new descriptor: %d/%v", protected, protectErr)
	}
	var after unix.Stat_t
	requireProgressNoError(t, unix.Fstat(descriptor, &after))
	afterStatus, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
	requireProgressNoError(t, err)
	afterFlags, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
	requireProgressNoError(t, err)
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Mode != after.Mode || status != afterStatus || afterFlags != flags|unix.FD_CLOEXEC {
		t.Fatal("descriptor protection changed owned pipe identity or flags")
	}
	count, err := unix.Write(descriptor, []byte(progressTestRecord))
	requireProgressNoError(t, err)
	if count != len(progressTestRecord) {
		t.Fatal("protected pipe did not acknowledge the complete record")
	}
}
