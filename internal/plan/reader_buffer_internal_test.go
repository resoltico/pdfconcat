// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package plan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// countedPumpSource observes whether read-ahead touches another chunk after buffer exhaustion.
type countedPumpSource struct {
	reader io.Reader
	reads  atomic.Int64
}

const pumpWaitLimit = 5 * time.Second

func (s *countedPumpSource) Read(destination []byte) (int, error) {
	s.reads.Add(1)

	length, err := s.reader.Read(destination)
	if err != nil {
		return length, fmt.Errorf("read counted pump source: %w", err)
	}

	return length, nil
}

func TestCancellationReleasesAPumpWaitingForExhaustedBuffers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	source := &countedPumpSource{reader: bytes.NewReader(bytes.Repeat([]byte("a"), 3*readChunkBytes))}
	reader := newChunkReader(cancellationOf(ctx), source)
	done := make(chan struct{})

	reader.startOnce.Do(func() { go func() { defer close(done); reader.pump() }() })

	var first [1]byte
	if length, err := reader.Read(first[:]); length != 1 || err != nil || first[0] != 'a' {
		t.Fatalf("first chunk: length=%d byte=%q err=%v", length, first, err)
	}
	// The consumer owns the first buffer and the full hand-off owns the second. With neither
	// returned, the next source read cannot begin; cancellation is the only ready free-buffer case.
	deadline := time.Now().Add(pumpWaitLimit)
	for len(reader.full) != 1 || len(reader.free) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("read-ahead did not exhaust the actual two-buffer pool")
		}

		runtime.Gosched()
	}

	cancel()

	select {
	case <-done:
	case <-time.After(pumpWaitLimit):
		t.Fatal("exhausted-buffer pump leaked after cancellation")
	}

	if got := source.reads.Load(); got != 2 {
		t.Fatalf("source reads=%d, want exactly the two owned buffers", got)
	}

	if length, err := reader.Read(first[:]); length != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled consumer: length=%d err=%v", length, err)
	}
}
