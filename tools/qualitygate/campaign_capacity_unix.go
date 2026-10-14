// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"fmt"
	"math"

	"golang.org/x/sys/unix"
)

func availableCampaignBytes(path string) (uint64, error) {
	var status unix.Statfs_t
	if err := unix.Statfs(path, &status); err != nil {
		return 0, err
	}
	if status.Bsize <= 0 || status.Bavail > math.MaxUint64/uint64(status.Bsize) {
		return 0, fmt.Errorf("invalid native filesystem capacity")
	}
	return status.Bavail * uint64(status.Bsize), nil
}

func campaignPlatformInspectionRace(error) bool { return false }
