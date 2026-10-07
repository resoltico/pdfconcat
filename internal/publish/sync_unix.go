// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package publish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// syncDirectory flushes the directory entry changes (a completed rename) to stable storage.
// Filesystems that cannot sync a directory report EINVAL or ENOTSUP; those are not failures
// because there is nothing further the program can do there.
func syncDirectory(dir string) error {
	// dir is the directory that was just published into, which the caller derived from the destination.
	handle, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}

	err = handle.Sync()
	if err != nil && !directorySyncUnsupported(err) {
		err = fmt.Errorf("sync directory: %w", err)
	} else {
		err = nil
	}

	return errors.Join(err, handle.Close())
}

func directorySyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP)
}
