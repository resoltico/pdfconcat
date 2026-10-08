//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"fmt"
	"os"
)

func createWorkspaceDirectory(parent, pattern string) (string, error) {
	prefix, err := privateTempPrefix(pattern)
	if err != nil {
		return "", err
	}

	path, err := os.MkdirTemp(parent, prefix+"*")
	if err != nil {
		return "", fmt.Errorf("create private workspace: %w", err)
	}

	return path, nil
}

// CreatePrivateTemp creates an exclusive owner-only temporary file before sensitive data is written.
func CreatePrivateTemp(dir, pattern string) (*os.File, error) {
	prefix, err := privateTempPrefix(pattern)
	if err != nil {
		return nil, err
	}

	file, err := os.CreateTemp(dir, prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("create private temporary file: %w", err)
	}

	return file, nil
}
