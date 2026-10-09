// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type (
	checkpointValueKey    struct{}
	formCheckpointContext struct {
		context.Context

		cancel    context.CancelFunc
		remaining atomic.Int64
	}
)

func newFormCheckpointContext(parent context.Context, t *testing.T) *formCheckpointContext {
	t.Helper()

	base, cancel := context.WithCancel(parent)
	t.Cleanup(cancel)

	return &formCheckpointContext{Context: base, cancel: cancel}
}

// CheckpointContext exposes the same real cancellation fixture to the external
// test package. The budget counts this wrapper's Err calls, not descendant work.
func CheckpointContext(parent context.Context, t *testing.T, budget int64) context.Context {
	t.Helper()
	ctx := newFormCheckpointContext(parent, t)
	ctx.remaining.Store(budget)

	return ctx
}

func (c *formCheckpointContext) Err() error {
	if c.remaining.Add(-1) < 0 {
		c.cancel()
	}

	return c.Context.Err()
}

func TestCheckpointContextClosesDoneAndKeepsStickyErrorAndValues(t *testing.T) {
	t.Parallel()

	parent := context.WithValue(t.Context(), checkpointValueKey{}, "kept")

	ctx := CheckpointContext(parent, t, 1)
	if err := ctx.Err(); err != nil {
		t.Fatalf("canceled before admitted checkpoint: %v", err)
	}

	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("actual checkpoint did not cancel: %v", err)
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("Err reported cancellation before real Done closed")
	}

	if err := ctx.Err(); !errors.Is(err, context.Canceled) || ctx.Value(checkpointValueKey{}) != "kept" {
		t.Fatalf("real cancellation or inherited value changed: %v", err)
	}
}

func TestCheckpointContextCannotMaskCanceledParentOrExpiredDeadline(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(t.Context())
	defer cancel()

	ctx := CheckpointContext(parent, t, 100)

	cancel()

	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("checkpoint budget masked independently canceled parent: %v", err)
	}

	deadline := time.Now().Add(-time.Second)

	parent, cancel = context.WithDeadline(t.Context(), deadline)
	defer cancel()

	ctx = CheckpointContext(parent, t, 100)

	actual, hasDeadline := ctx.Deadline()
	if !hasDeadline || !actual.Equal(deadline) || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("real inherited deadline changed: %v %t %v", actual, hasDeadline, ctx.Err())
	}
}

func TestCheckpointContextConcurrentCallsUseRealStickyCancellation(t *testing.T) {
	t.Parallel()

	ctx := CheckpointContext(t.Context(), t, 16)

	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("unexpected concurrent context error: %v", err)
			}

			_ = ctx.Value(checkpointValueKey{})
			_, _ = ctx.Deadline()
		})
	}

	workers.Wait()

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("concurrent checkpoint calls did not cancel real context")
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("real concurrent cancellation has open Done")
	}
}
