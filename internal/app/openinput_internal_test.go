// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenInputOpensAFileThatIsThere(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), planPath)

	err := os.WriteFile(path, []byte("{}"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	file, err := openInput(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}

	err = file.Close()
	if err != nil {
		t.Error(err)
	}

	_, err = openInput(t.Context(), filepath.Join(t.TempDir(), missingPlanPath))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
}

func TestOpenInputRejectsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := openInput(ctx, "missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled open: %v", err)
	}
}
