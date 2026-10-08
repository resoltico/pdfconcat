//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import "syscall"

const errDiskFull = syscall.ENOSPC
