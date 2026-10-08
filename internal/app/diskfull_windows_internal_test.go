// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package app

import "golang.org/x/sys/windows"

// errDiskFull is the error a full volume produces.
var errDiskFull error = windows.ERROR_DISK_FULL
