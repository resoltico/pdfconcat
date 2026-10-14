// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package report

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type decoderFixtureContext struct {
	context.Context
	parent      context.Context
	fixture     testing.TB
	limit       int64
	checkpoints atomic.Int64
}

// DecoderTestContext bounds finite fixture work and preserves parent cancellation.
// It is exported only in test builds to share this contract across both codec test packages.
func DecoderTestContext(parent context.Context, tb testing.TB) context.Context {
	tb.Helper()
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	tb.Cleanup(cancel)
	return &decoderFixtureContext{Context: ctx, parent: parent, fixture: tb, limit: DefaultLimits().MaxNodes}
}

func (c *decoderFixtureContext) Err() error {
	if err := c.parent.Err(); err != nil {
		return err
	}
	if err := c.Context.Err(); err != nil {
		// Parent cancellation can arrive between the two observations.
		if parentErr := c.parent.Err(); parentErr != nil {
			return parentErr
		}
		c.fixture.Fatalf("finite report fixture exceeded its existing two-minute decode fallback: %v", err)
	}
	// Allow one cancellation checkpoint per maximum admitted structural node.
	// The exact-MaxNodes report fixture independently exercises this bound.
	if c.checkpoints.Add(1) > c.limit {
		c.fixture.Fatalf("finite report fixture exceeded %d decode checkpoints without completing", c.limit)
	}
	return nil
}

func TestDecoderFixtureContextPreservesParentCancellation(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(t.Context())
	bounded := DecoderTestContext(parent, t)
	if err := bounded.Err(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := bounded.Err(); err != context.Canceled {
		t.Fatalf("parent cancellation changed: %v", err)
	}
	expired, release := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer release()
	if err := DecoderTestContext(expired, t).Err(); err != context.DeadlineExceeded {
		t.Fatalf("parent deadline changed: %v", err)
	}
}
