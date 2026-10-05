// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package publish

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func replaceFile(staged, destination string, overwrite bool) error {
	from, err := windows.UTF16PtrFromString(staged)
	if err != nil {
		return fmt.Errorf("encode staged path: %w", err)
	}

	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return fmt.Errorf("encode destination path: %w", err)
	}

	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if overwrite {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}

	err = windows.MoveFileEx(from, to, flags)
	if err != nil {
		return fmt.Errorf("move file: %w", err)
	}

	return nil
}
