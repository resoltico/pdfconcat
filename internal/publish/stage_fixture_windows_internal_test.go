//go:build windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// The substitution controls deliberately grant delete sharing so they can reach the identity guard.
// The production writer keeps Go's default sharing behavior.
func createDeleteSharedFixtureTemp(dir, pattern string) (*os.File, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("allocate substitution fixture: %w", err)
	}

	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		return nil, fmt.Errorf("close fixture allocator: %w", closeErr)
	}

	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode fixture path: %w", err)
	}

	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open delete-sharing fixture: %w", err)
	}

	return os.NewFile(uintptr(handle), path), nil
}
