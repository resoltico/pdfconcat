//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"fmt"
	"os"
)

func createDeleteSharedFixtureTemp(dir, pattern string) (*os.File, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("create delete-shared fixture: %w", err)
	}

	return file, nil
}
