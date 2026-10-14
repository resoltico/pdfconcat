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

	buffer, bufferSize, err := renameInformationBuffer(destination)
	if err != nil {
		return err
	}

	handle, err := windows.CreateFile(from, windows.DELETE, publicationFileShareMode,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fmt.Errorf("open staged rename handle: %w", err)
	}

	renameErr := windows.SetFileInformationByHandle(handle, windows.FileRenameInfoEx, &buffer[0], bufferSize)

	closeErr := windows.CloseHandle(handle)
	if renameErr != nil {
		return fmt.Errorf("replace open file: %w", errors.Join(renameErr, closeErr))
	}

	if closeErr != nil {
		return &FinalizationError{Path: destination, Err: fmt.Errorf("release renamed file handle: %w", closeErr)}
	}

	return nil
}

func renameInformationBuffer(destination string) ([]byte, uint32, error) {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve rename destination: %w", err)
	}

	name, err := windows.UTF16FromString(absolute)
	if err != nil {
		return nil, 0, fmt.Errorf("encode rename destination: %w", err)
	}

	nameBytes, bufferSize, err := renameInformationSizes(uint64(len(name)) - 1)
	if err != nil {
		return nil, 0, err
	}

	var header renameInformation

	offset := int(unsafe.Offsetof(header.FileName))
	buffer := make([]byte, bufferSize)
	binary.LittleEndian.PutUint32(buffer, windows.FILE_RENAME_REPLACE_IF_EXISTS|windows.FILE_RENAME_POSIX_SEMANTICS)
	binary.LittleEndian.PutUint32(buffer[unsafe.Offsetof(header.FileNameLength):], nameBytes)

	for index, value := range name {
		binary.LittleEndian.PutUint16(buffer[offset+index*2:], value)
	}

	return buffer, bufferSize, nil
}

// renameInformationSizes checks both native DWORD lengths and allocation width
// before constructing the native buffer. The path's UTF-16 conversion precedes it.
func renameInformationSizes(nameUnits uint64) (uint32, uint32, error) {
	const (
		offset        = int(unsafe.Offsetof(renameInformation{}.FileName))
		maxBufferSize = min(math.MaxUint32, math.MaxInt)
		maxNameUnits  = uint64((maxBufferSize - offset - 2) / 2)
	)
	if nameUnits > maxNameUnits {
		return 0, 0, errRenameInformationTooLarge
	}

	nameBytes := uint32(nameUnits) * 2
	bufferSize := max(uint32(unsafe.Sizeof(renameInformation{})), uint32(offset)+nameBytes+2)
	return nameBytes, bufferSize, nil
}
