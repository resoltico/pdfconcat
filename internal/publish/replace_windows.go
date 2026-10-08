// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package publish

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// renameInformation follows FILE_RENAME_INFO's native pointer alignment on both supported architectures.
type renameInformation struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

var errRenameInformationTooLarge = errors.New("rename information exceeds native buffer size")

func replaceFile(staged, destination string, existing existingFile) error {
	if existing == replaceExisting {
		return replaceOpenFile(staged, destination)
	}

	from, err := windows.UTF16PtrFromString(staged)
	if err != nil {
		return fmt.Errorf("encode staged path: %w", err)
	}

	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return fmt.Errorf("encode destination path: %w", err)
	}

	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)

	err = windows.MoveFileEx(from, to, flags)
	if err != nil {
		return fmt.Errorf("move file: %w", err)
	}

	return nil
}

// replaceOpenFile preserves the old file object held by recovery identity pins.
// Unsupported APIs/filesystems fail without a delete-and-move fallback.
func replaceOpenFile(staged, destination string) error {
	from, err := windows.UTF16PtrFromString(staged)
	if err != nil {
		return fmt.Errorf("encode staged path: %w", err)
	}

	buffer, err := renameInformationBuffer(destination)
	if err != nil {
		return err
	}

	bufferSize := len(buffer)
	if bufferSize < 0 || bufferSize > math.MaxUint32 {
		return errRenameInformationTooLarge
	}

	handle, err := windows.CreateFile(from, windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fmt.Errorf("open staged rename handle: %w", err)
	}

	renameErr := windows.SetFileInformationByHandle(handle, windows.FileRenameInfoEx, &buffer[0], uint32(bufferSize))

	closeErr := windows.CloseHandle(handle)
	if renameErr != nil {
		return fmt.Errorf("replace open file: %w", errors.Join(renameErr, closeErr))
	}

	if closeErr != nil {
		return &FinalizationError{Path: destination, Err: fmt.Errorf("release renamed file handle: %w", closeErr)}
	}

	return nil
}

func renameInformationBuffer(destination string) ([]byte, error) {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return nil, fmt.Errorf("resolve rename destination: %w", err)
	}

	name, err := windows.UTF16FromString(absolute)
	if err != nil {
		return nil, fmt.Errorf("encode rename destination: %w", err)
	}

	var header renameInformation

	nameBytes := (len(name) - 1) * 2
	if nameBytes < 0 || nameBytes > math.MaxUint32 {
		return nil, errRenameInformationTooLarge
	}

	offset := int(unsafe.Offsetof(header.FileName))
	buffer := make([]byte, max(int(unsafe.Sizeof(header)), offset+nameBytes+2))
	binary.LittleEndian.PutUint32(buffer, windows.FILE_RENAME_REPLACE_IF_EXISTS|windows.FILE_RENAME_POSIX_SEMANTICS)
	binary.LittleEndian.PutUint32(buffer[unsafe.Offsetof(header.FileNameLength):], uint32(nameBytes))

	for index, value := range name {
		binary.LittleEndian.PutUint16(buffer[offset+index*2:], value)
	}

	return buffer, nil
}
