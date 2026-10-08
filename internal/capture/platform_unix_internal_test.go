// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package capture

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

type infoWithoutIdentity struct{ fs.FileInfo }

func makeFIFO(path string) error {
	err := syscall.Mkfifo(path, testPermissions)
	if err != nil {
		return fmt.Errorf("make FIFO: %w", err)
	}

	return nil
}

func (infoWithoutIdentity) Sys() any { return nil }

func TestIdentityOfInfoRequiresOperatingSystemData(t *testing.T) {
	t.Parallel()

	_, err := identityOfInfo(infoWithoutIdentity{})
	if !errors.Is(err, errIdentityUnavailable) {
		t.Fatalf("identityOfInfo() = %v", err)
	}
}
