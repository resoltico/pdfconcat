// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package app

import "syscall"

// errDiskFull is the error a full volume produces.
var errDiskFull error = syscall.ENOSPC
