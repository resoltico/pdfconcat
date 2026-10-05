// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package publish enforces output policy and final same-filesystem publication.
package publish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// File exposes a verified staged file using platform-native rename semantics.
func File(staged, destination string, overwrite bool) error {
	if staged == "" || destination == "" {
		return errors.New("staged and destination paths are required")
	}

	err := ensureDestinationPolicy(destination, overwrite)
	if err != nil {
		return err
	}

	err = replaceFile(staged, destination, overwrite)
	if err != nil {
		return fmt.Errorf("publish %q: %w", destination, err)
	}

	return nil
}

// CheckDestination validates the destination directory and current overwrite policy.
func CheckDestination(destination string, overwrite bool) error {
	if destination == "" {
		return errors.New("output path is empty")
	}

	dir := filepath.Dir(destination)

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("output directory %q: %w", dir, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("output directory %q is not a directory", dir)
	}

	return ensureDestinationPolicy(destination, overwrite)
}

func ensureDestinationPolicy(destination string, overwrite bool) error {
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect output %q: %w", destination, err)
	}

	return checkExistingDestination(destination, info, overwrite)
}

// checkExistingDestination decides whether an existing destination may be replaced.
func checkExistingDestination(destination string, info os.FileInfo, overwrite bool) error {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("output %q is a symbolic link; choose a regular-file path", destination)
	case info.IsDir():
		return fmt.Errorf("output %q is a directory", destination)
	case !info.Mode().IsRegular():
		return fmt.Errorf("output %q is not a regular file", destination)
	case !overwrite:
		return fmt.Errorf("output %q already exists; use --overwrite to replace it", destination)
	default:
		return nil
	}
}
