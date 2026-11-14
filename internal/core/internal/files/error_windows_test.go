// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import "golang.org/x/sys/windows"

func unsupportedError() error { return windows.ERROR_NOT_SUPPORTED }
