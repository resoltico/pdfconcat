// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"strconv"
)

func fuzzWorkerStopped(pid int) bool {
	root, err := os.OpenRoot("/proc")
	if err != nil {
		return false
	}

	content, readErr := root.ReadFile(strconv.Itoa(pid) + "/stat")

	releaseErr := root.Close()
	if releaseErr != nil {
		return false
	}

	if errors.Is(readErr, os.ErrNotExist) {
		return true
	}

	if readErr != nil {
		return false
	}

	_, state, parseErr := fuzzLinuxProcessState(content)

	return parseErr == nil && (state == "Z" || state == "X")
}
