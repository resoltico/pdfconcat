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
	kind := unix.ByteSliceToString(stat.Fstypename[:])
	return kind == "msdos" || kind == "smbfs"
}
