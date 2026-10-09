// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func verifyLimitFixtureDescriptors(t *testing.T) {
	t.Helper()

	var actual unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &actual); err != nil {
		t.Fatal(err)
	}

	if actual.Cur != 64 || actual.Max != 64 {
		t.Fatalf("initialized Go runtime changed bound: %+v", actual)
	}

	raised := actual

	raised.Max++
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &raised); !errors.Is(err, unix.EPERM) {
		t.Fatalf("hard limit raise result: %v", err)
	}

	for _, fd := range []int{64, 83} {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatalf("inherited high descriptor %d survived: %v", fd, err)
		}
	}
}
