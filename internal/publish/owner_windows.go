// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// A metadata-only handle shares deletion so rename remains legal after the writer closes.
func openReportLease(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode report pin path: %w", err)
	}

	handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("open report identity pin: %w", err)
	}

	return os.NewFile(uintptr(handle), path), nil
}

// Handle.Stat initializes Windows file IDs. Avoid SameFile's lazy Lstat lookup, which uses exclusive sharing.
func checkReportNamespace(live, _ os.FileInfo, path string) error {
	current, err := openReportLease(path)
	if err != nil {
		return err
	}

	info, statErr := current.Stat()

	closeErr := current.Close()
	if statErr != nil || closeErr != nil {
		return fmt.Errorf("identify current report namespace: %w", errors.Join(statErr, closeErr))
	}

	return compareReportObjects(live, info, path)
}
