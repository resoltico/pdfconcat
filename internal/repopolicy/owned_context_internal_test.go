// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"context"
	"errors"
	"testing"
)

func TestOwnedSourceScansPreserveCallerCancellationAfterForeignCacheWarmup(t *testing.T) {
	t.Parallel()

	root, _ := tinyForeignSource(t)
	writeForeignTestFile(t, root, "app/main.go", []byte("package app\nfunc Value() int { return 1 }\n"))

	if _, err := OwnedGoSources(t.Context(), root); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	checks := []func() error{
		func() error { _, err := OwnedGoSources(ctx, root); return err },
		func() error { _, err := OwnedGoDirectories(ctx, root); return err },
		func() error { _, err := ScanSourceLimits(ctx, root); return err },
		func() error { _, err := ScanDirectives(ctx, root); return err },
	}
	for _, check := range checks {
		if err := check(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cached foreign identity detached source scan from caller cancellation: %v", err)
		}
	}
}
