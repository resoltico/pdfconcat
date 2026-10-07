//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import "golang.org/x/sys/windows"

const errDiskFull = windows.ERROR_DISK_FULL
