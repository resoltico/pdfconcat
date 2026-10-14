// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
	"io/fs"
	"slices"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// The Windows filesystem, rather than a lexical/symlink walker, selects the creation parent.
func nativeParentPath(parent string) (path string, failure error) {
	name, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(name, 0, identityShareMode, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer func() { failure = errors.Join(failure, windows.CloseHandle(handle)) }()
	// Bound the native path buffer around the documented extended-path range;
	// larger native results must fail rather than yield a truncated parent.
	var buffer [32768]uint16
	count, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	// The native wrapper reports every zero return as an error above.
	if count >= uint32(len(buffer)) {
		return "", windows.ERROR_INSUFFICIENT_BUFFER
	}
	units := buffer[:count]
	decoded := windows.UTF16ToString(units)
	encoded := utf16.Encode([]rune(decoded))
	if !slices.Equal(units, encoded) {
		return "", fs.ErrInvalid
	}
	return decoded, nil
}
