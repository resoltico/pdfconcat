// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageRefusesClosedCreatedHandleAndPreservesUnverifiedPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file, err := os.CreateTemp(dir, "owned-*")
	if err != nil {
		t.Fatal(err)
	}

	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	ops := realOperations()

	ops.createTemp = func(string, string) (*os.File, error) { return file, nil }

	_, stageErr := stageWith(t.Context(), ops, filepath.Join(dir, reportPath), strings.NewReader(stagedContent), 1000)
	if !errors.Is(stageErr, os.ErrClosed) {
		t.Fatalf("invalid created handle accepted: %v", stageErr)
	}

	if _, statErr := os.Stat(file.Name()); statErr != nil {
		t.Fatalf("unverified namespace removed: %v", statErr)
	}
}
