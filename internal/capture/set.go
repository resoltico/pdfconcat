// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Set owns one job's physical captures. Logical paths retain their own provenance.
// Concurrent aliases await one copy; unrelated copies never hold the catalog lock.
type Set struct {
	workspace *Workspace
	entries   map[string]*setEntry
	resources map[Identity]*setEntry
	mu        sync.Mutex
}
type setEntry struct {
	done     chan struct{}
	err      error
	captured Captured
}

func NewSet(workspace *Workspace) *Set {
	return &Set{workspace: workspace, entries: make(map[string]*setEntry), resources: make(map[Identity]*setEntry)}
}

// Capture returns one immutable private snapshot of the opened physical source.
func (s *Set) Capture(ctx context.Context, path string) (Captured, error) {
	return s.capture(ctx, path, 0)
}

// CaptureLimited enforces a positive byte limit before creating scratch content and while reading.
func (s *Set) CaptureLimited(ctx context.Context, path string, limit int64) (Captured, error) {
	if limit <= 0 {
		return Captured{}, fmt.Errorf("capture limit must be positive")
	}
	return s.capture(ctx, path, limit)
}

func (s *Set) capture(ctx context.Context, path string, limit int64) (Captured, error) {
	if err := ctx.Err(); err != nil {
		return Captured{}, fmt.Errorf(captureFailureFormat, path, err)
	}
	s.mu.Lock()
	if entry, ok := s.entries[path]; ok {
		s.mu.Unlock()
		return awaitCapture(ctx, path, limit, entry)
	}
	source, err := openSource(path)
	if err != nil {
		entry := &setEntry{done: make(chan struct{}), err: &SourceError{Path: path, Operation: "open source", Err: err}}
		close(entry.done)
		s.entries[path] = entry
		s.mu.Unlock()
		return Captured{}, entry.err
	}
	identity, err := IdentityOfFile(source)
	if err != nil {
		s.mu.Unlock()
		return Captured{}, errors.Join(err, closeSource(source, path))
	}
	if entry, ok := s.resources[identity]; ok {
		s.entries[path] = entry
		s.mu.Unlock()
		if closeErr := closeSource(source, path); closeErr != nil {
			return Captured{}, closeErr
		}
		return awaitCapture(ctx, path, limit, entry)
	}
	entry := &setEntry{done: make(chan struct{})}
	s.entries[path] = entry
	s.resources[identity] = entry
	s.mu.Unlock()
	entry.captured, entry.err = s.workspace.snapshotOpen(ctx, source, path, limit)
	entry.err = errors.Join(entry.err, closeSource(source, path))
	if entry.err != nil {
		entry.err = errors.Join(entry.err, removeScratch(entry.captured.Path))
		entry.captured = Captured{}
	}
	close(entry.done)
	return awaitCapture(ctx, path, limit, entry)
}

func awaitCapture(ctx context.Context, path string, limit int64, entry *setEntry) (Captured, error) {
	select {
	case <-entry.done:
		captured := entry.captured
		if entry.err != nil {
			return Captured{}, entry.err
		}
		if limit > 0 && captured.Size > limit {
			return Captured{}, &LimitError{Path: path, Limit: limit}
		}
		captured.SourcePath = path
		return captured, nil
	case <-ctx.Done():
		return Captured{}, fmt.Errorf(captureFailureFormat, path, ctx.Err())
	}
}
