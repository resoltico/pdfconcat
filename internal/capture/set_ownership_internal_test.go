// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func captureResult(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("independent capture blocked behind native I/O")
	}
}

func TestCaptureNativeIODoesNotHoldPromiseMapLock(t *testing.T) {
	for _, phase := range []string{"open", "identity"} {
		t.Run(phase, func(t *testing.T) {
			workspace := newTestWorkspace(t)
			set := NewSet(t.Context(), workspace)
			dir := t.TempDir()
			paths := []string{filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")}
			for _, path := range paths {
				writeTestFile(t, path, []byte(path))
			}
			if _, err := set.Capture(t.Context(), paths[1]); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var workers sync.WaitGroup
			defer func() { close(release); workers.Wait(); set.Close() }()
			if phase == "open" {
				set.open = func(path string) (*os.File, error) {
					if path == paths[0] {
						close(entered)
						<-release
					}
					return openSource(path)
				}
			} else {
				set.identify = func(file *os.File) (Identity, error) {
					if file.Name() == paths[0] {
						close(entered)
						<-release
					}
					return IdentityOfFile(file)
				}
			}
			launch := func(path string) <-chan error {
				done := make(chan error, 1)
				workers.Add(1)
				go func() { defer workers.Done(); _, err := set.Capture(t.Context(), path); done <- err }()
				return done
			}
			launch(paths[0])
			<-entered
			captureResult(t, launch(paths[1]))
			captureResult(t, launch(paths[2]))
		})
	}
}

func TestCaptureWaiterCancellationDoesNotOwnSharedCopy(t *testing.T) {
	workspace := newTestWorkspace(t)
	job, cancelJob := context.WithCancel(t.Context())
	defer cancelJob()
	set := NewSet(job, workspace)
	defer set.Close()
	source := filepath.Join(t.TempDir(), "source")
	writeTestFile(t, source, []byte("one physical copy"))
	alias := filepath.Join(filepath.Dir(source), "alias")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var copies atomic.Int64
	workspace.afterChunk = func(int64) { copies.Add(1); close(entered); <-release }
	owner := make(chan error, 1)
	go func() { _, err := set.Capture(t.Context(), source); owner <- err }()
	<-entered
	waiter, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := set.Capture(waiter, alias); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter cancellation: %v", err)
	}
	aliasDone := make(chan error, 1)
	go func() { _, err := set.Capture(t.Context(), alias); aliasDone <- err }()
	close(release)
	captureResult(t, owner)
	captureResult(t, aliasDone)
	first, err := set.Capture(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := set.Capture(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	if copies.Load() != 1 || first.Path != second.Path || first.SourcePath != source || second.SourcePath != alias {
		t.Fatal("identity election lost copy ownership or occurrence provenance")
	}
}

func TestCaptureCloseCancelsAndJoinsReservedOwner(t *testing.T) {
	workspace := newTestWorkspace(t)
	set := NewSet(t.Context(), workspace)
	source := filepath.Join(t.TempDir(), "source")
	writeTestFile(t, source, []byte("bytes"))
	entered, release := make(chan struct{}), make(chan struct{})
	set.open = func(path string) (*os.File, error) { close(entered); <-release; return openSource(path) }
	done := make(chan error, 1)
	go func() { _, err := set.Capture(t.Context(), source); done <- err }()
	<-entered
	closed := make(chan struct{})
	go func() { set.Close(); close(closed) }()
	// Wait for the shared job cancellation; closure may not finish until the native owner returns.
	<-set.ctx.Done()
	select {
	case <-closed:
		t.Fatal("close did not join its producer")
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("job cancellation lost: %v", err)
	}
	<-closed
	if _, err := set.Capture(t.Context(), source); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed admission accepted: %v", err)
	}
	entries, err := os.ReadDir(workspace.Dir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed owner left snapshot: %v/%v", entries, err)
	}
}
