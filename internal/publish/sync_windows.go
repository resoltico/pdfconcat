// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package publish

// syncDirectory is a no-op on Windows: file contents are flushed before rename,
// but directory-entry crash durability is not established by the selected APIs.
func syncDirectory(string) error { return nil }
