// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestSnapshotSourceCancellationPreventsCopiedInputs(t *testing.T) {
	t.Parallel()

	source := snapshotFixture(t, map[string]string{snapshotSourceFile: snapshotPackage})
	target := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if copied, err := copyListed(ctx, source, target, snapshotSourceFile+"\x00"); copied != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled snapshot copied=%d, err=%v", copied, err)
	}

	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled snapshot changed target: entries=%v err=%v", entries, err)
	}

	if _, err = foreignInputNames(ctx, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot foreign input selection ignored cancellation: %v", err)
	}
}
