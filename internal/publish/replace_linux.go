// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build linux

package publish

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func replaceFile(staged, destination string, overwrite bool) error {
	if overwrite {
		err := os.Rename(staged, destination)
		if err != nil {
			return fmt.Errorf("rename over existing destination: %w", err)
		}

		return nil
	}

	err := unix.Renameat2(
		unix.AT_FDCWD,
		staged,
		unix.AT_FDCWD,
		destination,
		unix.RENAME_NOREPLACE,
	)
	if err != nil {
		return fmt.Errorf("rename without replacing: %w", err)
	}

	return nil
}
