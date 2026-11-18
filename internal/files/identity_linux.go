// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import "golang.org/x/sys/unix"

const errNotDirectory = unix.ENOTDIR

func directoryMayUseDOSNames(path string) bool {
	var stat unix.Statfs_t
	if unix.Statfs(path, &stat) != nil {
		return true
	}
	switch stat.Type {
	case unix.MSDOS_SUPER_MAGIC, unix.CIFS_SUPER_MAGIC, unix.SMB_SUPER_MAGIC, unix.SMB2_SUPER_MAGIC:
		return true
	}
	return false
}
