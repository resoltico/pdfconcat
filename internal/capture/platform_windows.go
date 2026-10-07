// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build windows

package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// errNotDiskFile is returned for a named pipe, console or other device.
var errNotDiskFile = errors.New("not a disk file (pipe or device)")

// IsDiskFull reports whether err means the volume has no space left.
func IsDiskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}

// openSource opens path for reading and rejects anything that is not a disk file (a named pipe,
// a console or another device) before the caller can read from it. Reparse-point symbolic links
// are followed deliberately.
func openSource(path string) (*os.File, error) {
	// The path is the source the user named; opening it is the point, so it is only normalized.
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	fileType, typeErr := windows.GetFileType(windows.Handle(file.Fd()))
	if typeErr != nil || fileType != windows.FILE_TYPE_DISK {
		return nil, errors.Join(errNotDiskFile, file.Close())
	}

	return file, nil
}

func identityOfOpen(file *os.File, _ fs.FileInfo) (Identity, error) {
	return identityOfHandle(windows.Handle(file.Fd()))
}

// IdentityOf returns the identity of the object path resolves to, following symbolic links.
func IdentityOf(path string) (Identity, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Identity{}, fmt.Errorf("encode path: %w", err)
	}

	handle, err := windows.CreateFile(
		name,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return Identity{}, fmt.Errorf("inspect: %w", err)
	}

	identity, err := identityOfHandle(handle)

	closeErr := windows.CloseHandle(handle)
	if closeErr != nil {
		return Identity{}, errors.Join(err, fmt.Errorf("close file handle: %w", closeErr))
	}

	return identity, err
}

func identityOfHandle(handle windows.Handle) (Identity, error) {
	var info windows.ByHandleFileInformation

	err := windows.GetFileInformationByHandle(handle, &info)
	if err != nil {
		return Identity{}, fmt.Errorf("read file identity: %w", err)
	}

	const highShift = 32

	return Identity{
		volume: uint64(info.VolumeSerialNumber),
		index:  uint64(info.FileIndexHigh)<<highShift | uint64(info.FileIndexLow),
	}, nil
}
