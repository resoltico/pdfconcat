// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// errIdentityUnavailable is returned when the operating system exposes no file identity.
var errIdentityUnavailable = errors.New("filesystem identity is unavailable")

// IsDiskFull reports whether err means the volume or the user's quota has no space left.
func IsDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT)
}

// openSource opens path for reading without ever blocking: O_NONBLOCK makes opening a FIFO with
// no writer return immediately and O_NOCTTY stops a terminal device becoming the controlling
// terminal. Symbolic links are followed deliberately. The caller must fstat the handle.
func openSource(path string) (*os.File, error) {
	// The path is the source the user named; opening it is the point, so it is only normalized.
	file, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	return file, nil
}

func identityOfOpen(_ *os.File, info fs.FileInfo) (Identity, error) {
	return identityOfInfo(info)
}

// IdentityOf returns the identity of the object path resolves to, following symbolic links.
// It never opens the path, so it cannot block on a FIFO or device.
func IdentityOf(path string) (Identity, error) {
	identity, _, err := inspectIdentity(path)
	return identity, err
}

// inspectIdentity derives identity and directory kind from one native observation.
func inspectIdentity(path string) (Identity, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Identity{}, false, fmt.Errorf("inspect: %w", err)
	}

	identity, identityErr := identityOfInfo(info)

	return identity, info.IsDir(), identityErr
}

func identityOfInfo(info fs.FileInfo) (Identity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errIdentityUnavailable
	}

	return Identity{volume: widen(stat.Dev), index: widen(stat.Ino)}, nil
}

// widen converts a device or inode number to uint64; their types differ between Unix systems.
func widen[T ~int32 | ~uint32 | ~int64 | ~uint64](value T) uint64 {
	return uint64(value)
}
