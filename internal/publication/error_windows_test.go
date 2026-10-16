// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publication

import "golang.org/x/sys/windows"

func unsupportedError() error { return windows.ERROR_NOT_SUPPORTED }
