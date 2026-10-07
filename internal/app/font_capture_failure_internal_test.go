// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
)

func TestCanceledFontCaptureRetainsNoScratchBytes(t *testing.T) {
	t.Parallel()
	workspace := openScratch(t)
	source := filepath.Join(t.TempDir(), "font.ttf")

	if err := os.WriteFile(source, []byte("font candidate"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	current := &pipeline{registry: capture.NewRegistry(), captures: capture.NewSet(workspace), workspace: workspace}
	font, err := current.loadFont(ctx, &assembly.FontFileUse{Path: source})

	if font != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("font capture continued after cancellation: %v %v", font, err)
	}

	files, readErr := os.ReadDir(workspace.Dir())
	if readErr != nil || len(files) != 0 {
		t.Fatalf("canceled capture retained scratch: %v %v", files, readErr)
	}
}
