// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"fmt"
	"os"
	"syscall"
)

type objectID struct {
	device uint64
	inode  uint64
	kind   uint32
}

func identity(path string) (objectID, error) {
	info, err := os.Stat(path)
	if err != nil {
		return objectID{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return objectID{}, fmt.Errorf("unsupported file identity for %s", path)
	}
	return objectID{device: stat.Dev, inode: stat.Ino, kind: uint32(info.Mode().Type())}, nil
}
