//go:build darwin || linux

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProgressSinkConstructorOwnsMinimumTargetAndGuard(t *testing.T) {
	for _, role := range []string{"target", "guard"} {
		t.Run(role, func(t *testing.T) {
			if os.Getenv(progressTransportChild) != t.Name() {
				var reader, inherited *os.File
				if role == "target" {
					reader, inherited = progressPipe(t)
				} else {
					var err error
					inherited, err = os.Open(os.DevNull)
					requireProgressNoError(t, err)
					closeProgressResource(t, inherited)
				}
				if runProgressTransportChild(t, progressTransportChildBudget, inherited) {
					if reader != nil {
						requireProgressNoError(t, inherited.Close())
						data, err := io.ReadAll(reader)
						requireProgressNoError(t, err)
						if !bytes.Equal(data, []byte(progressTestRecord)) {
							t.Fatalf("minimum native target delivered %q", data)
						}
					}
					return
				}
			}
			if runProgressTransportChild(t, progressTransportChildBudget) {
				return
			}
			descriptor := unix.Stderr + 1 // ExtraFiles[0] reserves real fd 3 before runtime setup.
			_, err := unix.FcntlInt(uintptr(descriptor), unix.F_SETFD, unix.FD_CLOEXEC)
			requireProgressNoError(t, err)
			sink := progressNativeSink{fd: descriptor, guard: -1}
			if role == "guard" {
				sink.guard = descriptor
				sink.fd, err = unix.Open("/dev/null", unix.O_WRONLY|unix.O_CLOEXEC, 0)
				requireProgressNoError(t, err)
			}
			owned := map[int]bool{sink.fd: true}
			if sink.guard != -1 {
				owned[sink.guard] = true
			}
			t.Cleanup(func() {
				for fd, live := range owned {
					if live {
						if err := unix.Close(fd); err != nil && !errors.Is(err, unix.EBADF) {
							t.Error(err)
						}
					}
				}
			})
			transport, err := progressTransportFromSink(sink)
			if err != nil || transport == nil {
				t.Fatalf("real minimum %s descriptor refused: %v", role, err)
			}
			closeProgressResource(t, transport)
			requireProgressNoError(t, transport.WriteRecord(t.Context(), []byte(progressTestRecord)))
			requireProgressNoError(t, transport.Close())
			for fd := range owned {
				var stat unix.Stat_t
				closedErr := unix.Fstat(fd, &stat)
				if errors.Is(closedErr, unix.EBADF) {
					owned[fd] = false
				} else {
					t.Errorf("owned minimum %s acquisition retained descriptor %d: %v", role, fd, closedErr)
				}
			}
		})
	}
}
