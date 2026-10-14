// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

func availableCampaignBytes(path string) (uint64, error) {
	if path == "" {
		return 0, windows.ERROR_INVALID_NAME
	}
	name, err := windows.UTF16PtrFromString(campaignCapacityDirectory(path))
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	err = windows.GetDiskFreeSpaceEx(name, &available, &total, &free)
	return available, err
}

// GetDiskFreeSpaceExW requires a trailing backslash for UNC directory names.
// https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getdiskfreespaceexw
// Append without cleaning native traversal or turning an invalid empty path
// into the current volume's root.
func campaignCapacityDirectory(path string) string {
	if path != "" && path[len(path)-1] != '\\' {
		return path + `\`
	}
	return path
}

func campaignPlatformInspectionRace(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
