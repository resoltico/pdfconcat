//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"errors"
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

	if accessErr := removeInheritedAccess(path); accessErr != nil {
		return "", errors.Join(accessErr, os.Remove(path))
	}
	return path, nil
}

// CreatePrivateTemp creates an exclusive owner-only temporary file before sensitive data is written.
func CreatePrivateTemp(dir, pattern string) (*os.File, error) {
	if dir != "" {
		if err := validateBoundPath(dir); err != nil {
			return nil, &os.PathError{Op: "validate private parent", Path: dir, Err: err}
		}
	}
	prefix, err := privateTempPrefix(pattern)
	if err != nil {
		return nil, err
	}

	file, err := os.CreateTemp(dir, prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("create private temporary file: %w", err)
	}

	if accessErr := removeInheritedAccess(file.Name()); accessErr != nil {
		return nil, errors.Join(accessErr, file.Close(), os.Remove(file.Name()))
	}
	return file, nil
}
