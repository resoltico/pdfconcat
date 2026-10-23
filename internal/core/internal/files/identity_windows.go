// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/windows"
)

type objectID struct {
	volume uint64
	file   [16]byte
}

func identity(path string) (objectID, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return objectID{}, identityError("UTF16PtrFromString", path, err)
	}
	handle, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return objectID{}, identityError("CreateFile", path, err)
	}
	// FILE_ID_INFO is an eight-byte volume serial followed by FILE_ID_128.
	// A byte buffer avoids Go/native struct layout and unsafe pointer assumptions.
	var data [8 + 16]byte
	queryErr := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, &data[0], uint32(len(data)))
	closeErr := windows.CloseHandle(handle)
	if err := errors.Join(identityError("GetFileInformationByHandleEx(FileIdInfo)", path, queryErr), identityError("CloseHandle", path, closeErr)); err != nil {
		return objectID{}, err
	}
	id := objectID{volume: binary.LittleEndian.Uint64(data[:8])}
	copy(id.file[:], data[8:])
	return id, nil
}
