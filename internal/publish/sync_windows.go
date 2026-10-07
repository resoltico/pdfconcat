// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package publish

// syncDirectory does nothing on Windows: directories cannot be flushed through a handle, and
// MoveFileEx is called with MOVEFILE_WRITE_THROUGH so the rename itself is flushed before it
// returns.
func syncDirectory(string) error { return nil }
