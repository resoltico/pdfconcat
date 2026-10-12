// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
)

// Set owns one job's physical captures. Logical paths retain their own provenance.
// Concurrent aliases await one copy; unrelated copies never hold the catalog lock.
type Set struct {
	workspace *Workspace
	entries   map[string]*setEntry
	resources map[Identity]*setEntry
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	owners    sync.WaitGroup
	closed    bool
	open      func(string) (*os.File, error)
	identify  func(*os.File) (Identity, error)
}
type setEntry struct {
	done     chan struct{}
	err      error
	captured Captured
}

func NewSet(ctx context.Context, workspace *Workspace) *Set {
	owned, cancel := context.WithCancel(ctx)
	return &Set{ctx: owned, cancel: cancel, workspace: workspace, entries: make(map[string]*setEntry), resources: make(map[Identity]*setEntry), open: openSource, identify: IdentityOfFile}
}

// Close stops admission, cancels shared work and joins producers before the caller closes its workspace.
// An arbitrary blocked native call is not guaranteed interruptible.
func (s *Set) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.owners.Wait()
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
	if s.closed {
		s.mu.Unlock()
		return Captured{}, &SourceError{Path: path, Operation: "capture admission", Err: os.ErrClosed}
	}
	if entry, ok := s.entries[path]; ok {
		s.mu.Unlock()
		return awaitCapture(ctx, path, limit, entry)
	}
	entry := &setEntry{done: make(chan struct{})}
	s.entries[path] = entry
	s.owners.Add(1)
	s.mu.Unlock()
	// The elected caller owns path publication synchronously; no per-resource goroutine is created.
	defer s.owners.Done()
	entry.captured, entry.err = s.captureOwner(path, limit)
	close(entry.done)
	return awaitCapture(ctx, path, limit, entry)
}

func (s *Set) captureOwner(path string, limit int64) (Captured, error) {
	if err := s.ctx.Err(); err != nil {
		return Captured{}, &SourceError{Path: path, Operation: "capture source", Err: err}
	}
	source, err := s.open(path)
	if err != nil {
		return Captured{}, &SourceError{Path: path, Operation: "open source", Err: err}
	}
	if err := s.ctx.Err(); err != nil {
		return Captured{}, errors.Join(&SourceError{Path: path, Operation: "capture source", Err: err}, closeSource(source, path))
	}
	identity, err := s.identify(source)
	if err != nil {
		return Captured{}, errors.Join(&SourceError{Path: path, Operation: "identify source", Err: err}, closeSource(source, path))
	}
	s.mu.Lock()
	resource, found := s.resources[identity]
	if !found {
		resource = &setEntry{done: make(chan struct{})}
		s.resources[identity] = resource
	}
	s.mu.Unlock()
	if found {
		// The identity loser closes only its own redundant descriptor, then waits outside the map lock.
		if err := closeSource(source, path); err != nil {
			return Captured{}, err
		}
		return awaitCapture(s.ctx, path, limit, resource)
	}
	// Exactly one physical owner copies, closes its source, removes failures, and publishes an immutable result.
	resource.captured, resource.err = s.workspace.snapshotOpen(s.ctx, source, path, limit)
	resource.err = errors.Join(resource.err, closeSource(source, path))
	if resource.err != nil {
		resource.err = errors.Join(resource.err, removeScratch(resource.captured.Path))
		resource.captured = Captured{}
	}
	close(resource.done)
	return awaitCapture(s.ctx, path, limit, resource)
}

func awaitCapture(ctx context.Context, path string, limit int64, entry *setEntry) (Captured, error) {
	select {
	case <-entry.done:
		captured := entry.captured
		if entry.err != nil {
			return Captured{}, captureCallerError(path, entry.err)
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

func captureCallerError(path string, err error) error {
	if source, ok := errors.AsType[*SourceError](err); ok && source.Path == path {
		return err
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := joined.Unwrap()
		rebound := make([]error, len(parts))
		for i, part := range parts {
			rebound[i] = captureCallerError(path, part)
		}
		return errors.Join(rebound...)
	}
	switch failure := err.(type) {
	case *SourceError:
		return &SourceError{Path: path, Operation: failure.Operation, Err: failure.Err}
	case *NotRegularFileError:
		return &NotRegularFileError{Path: path, Kind: failure.Kind}
	case *LimitError:
		return &LimitError{Path: path, Limit: failure.Limit}
	case *SourceChangedError:
		return &SourceChangedError{Path: path}
	}
	return err
}
