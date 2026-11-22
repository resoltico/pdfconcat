// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux || darwin

package files

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func unsupportedRename(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOSYS)
}
func syncDirectory(parent *os.Root) error {
	f, err := parent.Open(".")
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
