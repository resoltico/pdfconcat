// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// executableCoverageInputs includes every completed child of the gate's private coverage root.
// Go validates the binary data; this walk enforces the owned, regular, one-level input boundary.
func executableCoverageInputs(root string) ([]string, error) {
	inputs, children, err := coverageDirectoryInputs(root)
	if err != nil {
		return nil, err
	}

	for _, child := range children {
		data, nested, childErr := coverageDirectoryInputs(child)
		if childErr != nil {
			return nil, childErr
		}

		if len(nested) != 0 {
			return nil, fmt.Errorf("%w: nested executable coverage directory: %s", errGate, child)
		}

		if len(data) == 0 {
			return nil, fmt.Errorf("%w: child produced no executable coverage: %s", errGate, child)
		}

		inputs = append(inputs, data...)
	}

	if len(inputs) == 0 {
		return nil, fmt.Errorf("%w: tests produced no executable coverage in %s", errGate, root)
	}

	return inputs, nil
}

func coverageDirectoryInputs(directory string) ([]string, []string, error) {
	if err := requireCoverageDirectory(directory); err != nil {
		return nil, nil, err
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, nil, fmt.Errorf("read executable coverage directory: %w", err)
	}

	var children []string

	metadata, counters := false, false

	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			children = append(children, path)
			continue
		}

		kind, fileErr := coverageFileKind(path, entry)
		if fileErr != nil {
			return nil, nil, fileErr
		}

		metadata = metadata || kind == "metadata"
		counters = counters || kind == "counters"
	}

	if metadata != counters {
		return nil, nil, fmt.Errorf("%w: incomplete executable coverage data: %s", errGate, directory)
	}

	if metadata {
		return []string{directory}, children, nil
	}

	return nil, children, nil
}

func requireCoverageDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect executable coverage directory: %w", err)
	}

	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: coverage input is not a regular directory: %s", errGate, directory)
	}

	return nil
}

func coverageFileKind(path string, entry os.DirEntry) (string, error) {
	file, err := entry.Info()
	if err != nil {
		return "", fmt.Errorf("inspect executable coverage input: %w", err)
	}

	if !file.Mode().IsRegular() {
		return "", fmt.Errorf("%w: nonregular executable coverage input: %s", errGate, path)
	}

	switch {
	case strings.HasPrefix(entry.Name(), "covmeta."):
		return "metadata", nil
	case strings.HasPrefix(entry.Name(), "covcounters."):
		return "counters", nil
	default:
		return "", fmt.Errorf("%w: unexpected executable coverage input: %s", errGate, path)
	}
}
