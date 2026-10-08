// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"fmt"
	"os"
)

// Identity names one filesystem object independent of the path used to reach it: the device and
// inode on Unix, the volume serial number and file index on Windows. Hard links, symbolic links
// and case aliases of one file share an Identity. It is comparable and usable as a map key.
type Identity struct {
	volume uint64
	index  uint64
}

// String renders the identity as "volume:index" in hexadecimal for reports.
func (i Identity) String() string {
	return fmt.Sprintf("%x:%x", i.volume, i.index)
}

// IdentityOfFile identifies the already-open object; a path replacement cannot change this identity.
func IdentityOfFile(file *os.File) (Identity, error) {
	info, err := file.Stat()
	if err != nil {
		return Identity{}, fmt.Errorf("inspect open file: %w", err)
	}

	return identityOfOpen(file, info)
}
