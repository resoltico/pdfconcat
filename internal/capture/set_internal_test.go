// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type (
	// scratchCounter wraps scratch files so a test can observe how many are open at once.
	scratchCounter struct {
		open atomic.Int64
		peak atomic.Int64
	}

	countedScratch struct {
		io.WriteCloser

		counter *scratchCounter
	}
)

func (c *scratchCounter) openScratch(path string) (io.WriteCloser, error) {
	file, err := openExclusiveScratch(path)
	if err != nil {
		return nil, err
	}

	now := c.open.Add(1)

	for {
		peak := c.peak.Load()
		if now <= peak || c.peak.CompareAndSwap(peak, now) {
			break
		}
	}

	return countedScratch{WriteCloser: file, counter: c}, nil
}

func (s countedScratch) Close() error {
	s.counter.open.Add(-1)

	err := s.WriteCloser.Close()
	if err != nil {
		return fmt.Errorf("close counted scratch: %w", err)
	}

	return nil
}

func TestSetCopiesEachDistinctPathOnce(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	set := NewSet(workspace)
	source := filepath.Join(t.TempDir(), "in.pdf")
	writeTestFile(t, source, []byte("shared"))

	const callers = 40

	results := make([]Captured, callers)

	var group sync.WaitGroup

	for index := range callers {
		group.Go(func() {
			captured, err := set.Capture(context.Background(), source)
			if err != nil {
				t.Error(err)
			}

			results[index] = captured
		})
	}

	group.Wait()

	for _, result := range results {
		if result != results[0] {
			t.Fatalf("results differ: %+v vs %+v", result, results[0])
		}
	}

	if scratchEntries(t, workspace) != 1 {
		t.Fatalf("scratch entries = %d, want one copy", scratchEntries(t, workspace))
	}
}

func TestSetRemembersFailures(t *testing.T) {
	t.Parallel()

	set := NewSet(newTestWorkspace(t))
	missing := filepath.Join(t.TempDir(), missingPDFPath)

	_, first := set.Capture(context.Background(), missing)
	_, second := set.Capture(context.Background(), missing)

	if !errors.Is(first, fs.ErrNotExist) || !errors.Is(first, second) {
		t.Fatalf("errors %v, %v", first, second)
	}
}

func TestSetWaiterStopsWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	set := NewSet(workspace)
	source := filepath.Join(t.TempDir(), "in.pdf")
	writeTestFile(t, source, []byte(fixtureContent))

	entered, release := make(chan struct{}), make(chan struct{})

	var once sync.Once

	workspace.afterChunk = func(int64) {
		once.Do(func() { close(entered) })
		<-release
	}

	firstDone := make(chan error, 1)

	go func() {
		_, err := set.Capture(context.Background(), source)
		firstDone <- err
	}()

	select {
	case <-entered:
	case err := <-firstDone:
		t.Fatalf("the first capture finished without copying a chunk: %v", err)
	case <-time.After(waitLimit):
		t.Fatal("the first capture never started copying")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := set.Capture(ctx, source)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter error = %v", err)
	}

	close(release)

	err = <-firstDone
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetCapturesManyDistinctFilesConcurrentlyWithBoundedScratchFiles(t *testing.T) {
	t.Parallel()

	const (
		files   = 300
		workers = 8
	)

	workspace := newTestWorkspace(t)
	counter := &scratchCounter{}
	workspace.openScratch = counter.openScratch
	set := NewSet(workspace)
	paths, digests := writeDistinctFiles(t, files)

	jobs := make(chan int)

	var group sync.WaitGroup

	for range workers {
		group.Go(func() {
			for index := range jobs {
				captured, err := set.Capture(context.Background(), paths[index])
				if err != nil || captured.SHA256 != digests[index] {
					t.Errorf("file %d: %+v, %v", index, captured, err)
				}
			}
		})
	}

	for index := range files {
		jobs <- index
	}

	close(jobs)
	group.Wait()

	if scratchEntries(t, workspace) != files {
		t.Fatalf("scratch entries = %d, want %d", scratchEntries(t, workspace), files)
	}

	if peak := counter.peak.Load(); peak > workers {
		t.Fatalf("peak open scratch files %d exceeds %d workers", peak, workers)
	}

	if open := counter.open.Load(); open != 0 {
		t.Fatalf("%d scratch files left open", open)
	}
}

// writeDistinctFiles writes count files with distinct content and returns their paths and digests.
func writeDistinctFiles(t *testing.T, count int) ([]string, [][sha256.Size]byte) {
	t.Helper()

	dir := t.TempDir()
	paths := make([]string, count)
	digests := make([][sha256.Size]byte, count)

	for index := range count {
		paths[index] = filepath.Join(dir, fmt.Sprintf("f%03d.pdf", index))
		content := fmt.Appendf(nil, "distinct content %d", index)
		digests[index] = sha256.Sum256(content)

		writeTestFile(t, paths[index], content)
	}

	return paths, digests
}
