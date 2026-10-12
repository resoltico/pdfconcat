// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"io/fs"
	"slices"

	"golang.org/x/sys/windows"
)

// The Windows filesystem, rather than a lexical/symlink walker, selects the creation parent.
func nativeParentPath(parent string) (path string, failure error) {
	name, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { failure = errors.Join(failure, windows.CloseHandle(handle)) }()
	// The Windows extended-path maximum is 32,767 UTF-16 units, including the terminal NUL.
	var buffer [32768]uint16
	count, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	if count == 0 || count >= uint32(len(buffer)) {
		return "", windows.ERROR_INSUFFICIENT_BUFFER
	}
	units := buffer[:count]
	decoded := windows.UTF16ToString(units)
	encoded, err := windows.UTF16FromString(decoded)
	if err != nil || !slices.Equal(units, encoded[:len(encoded)-1]) {
		return "", fs.ErrInvalid
	}
	return decoded, nil
}
