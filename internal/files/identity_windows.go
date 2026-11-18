// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import "golang.org/x/sys/windows"

const errNotDirectory = windows.ERROR_DIRECTORY

func directoryMayUseDOSNames(string) bool { return true }
