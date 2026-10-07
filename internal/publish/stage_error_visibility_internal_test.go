// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stagedContentReader func([]byte) (int, error)

func (r stagedContentReader) Read(p []byte) (int, error) { return r(p) }

func TestStagingPreservesSimultaneousCopyAndCloseFailures(t *testing.T) {
	t.Parallel()

	file, createErr := os.CreateTemp(t.TempDir(), "stage-*")
	if createErr != nil {
		t.Fatal(createErr)
	}

	ops := realOperations()
	ops.writeChunk = func(_ io.Writer, _ []byte) error {
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}

		return errInjected
	}

	_, err := fillStaged(t.Context(), ops, file, strings.NewReader(stagedContent), 100)
	if !errors.Is(err, errInjected) || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("copy/close error lost: %v", err)
	}
}

func TestStagingSizeLimitRetainsOwnershipCleanupFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ops := realOperations()

	var path string

	original := createDeleteSharedFixtureTemp
	ops.createTemp = func(directory, pattern string) (*os.File, error) {
		file, err := original(directory, pattern)
		if err == nil {
			path = file.Name()
		}

		return file, err
	}
	reader := stagedContentReader(func(p []byte) (int, error) {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}

		put(t, path, unknownReplacement)

		return copy(p, "exceeds limit"), io.EOF
	})
	target := filepath.Join(dir, reportPath)
	_, err := stageWith(t.Context(), ops, target, reader, 1)

	limit, isLimit := errors.AsType[*SizeLimitError](err)
	if !isLimit || !errors.Is(err, errOwnerChanged) {
		t.Fatalf("limit/cleanup error lost: %v", err)
	}

	if limit.Target != target || limit.Limit != 1 {
		t.Fatalf("limit metadata: %+v", limit)
	}

	if get(t, path) != unknownReplacement {
		t.Fatal("foreign entry removed")
	}
}
