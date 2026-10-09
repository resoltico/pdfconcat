// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestForeignSourceCallerCancellationPreventsCachedSuccessAndStageWrites(t *testing.T) {
	t.Parallel()

	root, source := tinyForeignSource(t)
	if _, err := ForeignSourcesContext(t.Context(), root); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := ForeignSourcesContext(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached source verification ignored cancellation: %v", err)
	}

	destination := t.TempDir()
	if err := ReconstructForeignModule(ctx, root, source, destination); !errors.Is(err, context.Canceled) {
		t.Fatalf("source reconstruction ignored cancellation: %v", err)
	}

	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Fatal("canceled source reconstruction wrote staged input")
	}
}
