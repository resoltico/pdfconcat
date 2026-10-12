//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import "path/filepath"

// EvalSymlinks traverses POSIX components before returning the reached directory spelling.
func nativeParentPath(parent string) (string, error) { return filepath.EvalSymlinks(parent) }
