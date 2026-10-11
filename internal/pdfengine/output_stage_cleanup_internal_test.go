// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdffixture"
)

func TestBackendPlacementFailureCleansStageWithoutInventingCleanupError(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	destination := filepath.Join(parent, "occupied")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}

	built := pool{pdf: readUnvalidated(t, pdffixture.Plain("P"))}
	digest, err := built.write(t.Context(), destination)
	if err == nil || !strings.Contains(err.Error(), "place staged output") || strings.Contains(err.Error(), "remove staged output") || digest != "" {
		t.Fatalf("failed placement invented success or cleanup failure: digest=%q err=%v", digest, err)
	}
	entries, readErr := os.ReadDir(parent)
	if readErr != nil || len(entries) != 1 || entries[0].Name() != "occupied" || !entries[0].IsDir() {
		t.Fatalf("staged output survived failed placement: entries=%v err=%v", entries, readErr)
	}
}
