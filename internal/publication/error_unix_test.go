// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux || darwin

package publication

import "golang.org/x/sys/unix"

func unsupportedError() error { return unix.ENOTSUP }
