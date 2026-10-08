// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package publish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/capture"
)

func TestStagePinRejectsSubstitutedFIFOWithoutWaitingForWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ops := realOperations()

	var writer *os.File

	ops.createTemp = func(directory, pattern string) (*os.File, error) {
		file, err := os.CreateTemp(directory, pattern)
		if err != nil {
			return nil, fmt.Errorf(createFixtureErrorFormat, err)
		}

		writer = file
		if removeErr := os.Remove(file.Name()); removeErr != nil {
			return nil, errors.Join(removeErr, file.Close())
		}

		if fifoErr := syscall.Mkfifo(file.Name(), 0o600); fifoErr != nil {
			return nil, errors.Join(fifoErr, file.Close())
		}

		return file, nil
	}
	done := make(chan error, 1)

	go func() {
		staged, err := stageWith(t.Context(), ops, filepath.Join(dir, reportPath), strings.NewReader(reportContent), 1000)
		if staged != nil {
			err = errors.Join(err, staged.Discard())
		}

		done <- err
	}()

	select {
	case err := <-done:
		irregular, isIrregular := errors.AsType[*capture.NotRegularFileError](err)
		if !isIrregular || irregular.Kind != "a named pipe" {
			t.Fatalf("FIFO not rejected: %v", err)
		}

		requireLeaseClosed(t, writer)
	case <-time.After(10 * time.Second):
		t.Fatal("identity pin blocked on a FIFO without a writer")
	}
}
