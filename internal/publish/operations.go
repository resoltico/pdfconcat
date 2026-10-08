// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/capture"
)

type (
	// existingFile says whether publication may replace a file already at the destination.
	existingFile int

	// operations are the filesystem primitives publication depends on. Production uses the real
	// ones; tests substitute failing versions to prove error handling and cleanup.
	operations struct {
		// replace is the native rename: no-clobber unless existing is replaceExisting.
		replace func(staged, destination string, existing existingFile) error
		// syncFile flushes a file's contents to stable storage.
		syncFile func(path string) error
		// syncDirectory flushes a directory's entries to stable storage.
		syncDirectory func(dir string) error
		// createTemp creates an exclusive private file in dir.
		createTemp func(dir, pattern string) (*os.File, error)
		// writeChunk writes one chunk of staged content.
		writeChunk func(dst io.Writer, chunk []byte) error
	}
)

const (
	// refuseExisting makes publication fail when the destination exists.
	refuseExisting existingFile = iota
	// replaceExisting lets publication replace an existing regular file.
	replaceExisting
)

func realOperations() operations {
	return operations{
		replace:       replaceFile,
		syncFile:      syncFile,
		syncDirectory: syncDirectory,
		createTemp:    capture.CreatePrivateTemp,
		writeChunk:    writeChunk,
	}
}

// syncFile opens path read-write (Windows requires write access to flush) and flushes it. The
// path is a staged file this package created or one the caller named.
func syncFile(path string) error {
	handle, err := os.OpenFile(filepath.Clean(path), os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open for sync: %w", err)
	}

	err = handle.Sync()
	if err != nil {
		err = fmt.Errorf("sync file: %w", err)
	}

	return errors.Join(err, handle.Close())
}

func writeChunk(dst io.Writer, chunk []byte) error {
	_, err := dst.Write(chunk)
	if err != nil {
		return fmt.Errorf("write chunk: %w", err)
	}

	return nil
}
