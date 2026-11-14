// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux || darwin

package files

import "golang.org/x/sys/unix"

func unsupportedError() error { return unix.ENOTSUP }
