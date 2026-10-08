// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"context"
	"fmt"
	"sync"
)

type (
	// Set captures each distinct normalized path of a job once, however many times and from however
	// many goroutines it is requested. Sources and fonts share one Set when they share a Workspace.
	// Normalizing equivalent spellings of one path is the caller's responsibility; the Set keys on
	// the string it is given. A Set is safe for concurrent use.
	Set struct {
		workspace *Workspace
		entries   map[string]*setEntry
		mu        sync.Mutex
	}

	setEntry struct {
		done     chan struct{}
		err      error
		captured Captured
	}
)

// NewSet returns an empty Set that copies into workspace.
func NewSet(workspace *Workspace) *Set {
	return &Set{workspace: workspace, entries: make(map[string]*setEntry)}
}

// Capture returns the private copy of path, copying it on the first request only. Concurrent
// requests for the same path wait for that one copy; a request whose context ends while waiting
// returns the context error. A failed capture is remembered, so every request for the path in
// this job reports the same failure.
func (s *Set) Capture(ctx context.Context, path string) (Captured, error) {
	s.mu.Lock()

	entry, found := s.entries[path]
	if !found {
		entry = &setEntry{done: make(chan struct{})}
		s.entries[path] = entry
	}

	s.mu.Unlock()

	if !found {
		entry.captured, entry.err = s.workspace.Snapshot(ctx, path)
		close(entry.done)

		return entry.captured, entry.err
	}

	select {
	case <-entry.done:
		return entry.captured, entry.err
	case <-ctx.Done():
		return Captured{}, fmt.Errorf(captureFailureFormat, path, ctx.Err())
	}
}
