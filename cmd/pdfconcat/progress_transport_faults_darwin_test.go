// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"errors"
	"math"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProgressDarwinInvalidNativeOwnershipFailsVisibly(t *testing.T) {
	t.Parallel()

	if path, err := progressTerminalPath(uintptr(math.MaxInt32)); path != "" || !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid native terminal path: path=%q error=%v", path, err)
	}

	if _, err := openProgressPipe(uintptr(math.MaxInt32)); !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid native pipe duplication: %v", err)
	}

	if err := interruptProgressPipe(-1, -1); !errors.Is(err, unix.EBADF) {
		t.Fatalf("failed native pipe interruption hidden: %v", err)
	}
}
