// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"syscall"
)

func fuzzWorkerStopped(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}
