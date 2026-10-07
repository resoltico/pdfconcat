// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/capture"
)

// mutationCaptureReader opens retained regular artifacts without blocking on substituted FIFOs.
func mutationCaptureReader(directory string) func(string) (io.ReadCloser, error) {
	return func(name string) (io.ReadCloser, error) {
		if !fs.ValidPath(name) || filepath.Base(name) != name {
			return nil, fmt.Errorf("%w: invalid mutation capture name %q", errGate, name)
		}

		path := filepath.Join(directory, name)

		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect mutation capture: %w", err)
		}

		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: mutation capture is not a regular file: %s", errGate, path)
		}

		file, err := capture.OpenRegular(path)
		if err != nil {
			return nil, fmt.Errorf("open mutation capture: %w", err)
		}

		opened, statErr := file.Stat()
		if statErr != nil || !os.SameFile(info, opened) {
			return nil, errors.Join(fmt.Errorf("%w: mutation capture identity changed: %s", errGate, path), statErr, file.Close())
		}

		current, inspectErr := os.Lstat(path)
		if inspectErr != nil || !current.Mode().IsRegular() || !os.SameFile(current, opened) {
			return nil, errors.Join(fmt.Errorf("%w: mutation capture namespace changed: %s", errGate, path), inspectErr, file.Close())
		}

		return file, nil
	}
}
