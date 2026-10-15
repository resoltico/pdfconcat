// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux || darwin

package atomicfile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func unsupportedRename(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOSYS)
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if errors.Is(err, unix.EINVAL) {
		err = nil
	}
	return errors.Join(err, closeErr)
}
