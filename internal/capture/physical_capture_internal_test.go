// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPhysicalAliasesShareOneConcurrentCapture(t *testing.T) {
	workspace := newTestWorkspace(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	writeTestFile(t, source, []byte("one physical document"))
	alias := filepath.Join(dir, "alias.pdf")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	var copies atomic.Int64
	workspace.openScratch = func(path string) (io.WriteCloser, error) { copies.Add(1); return openExclusiveScratch(path) }
	set := NewSet(t.Context(), workspace)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var group sync.WaitGroup
	for index := range 40 {
		group.Go(func() {
			path := source
			if index%2 != 0 {
				path = alias
			}
			captured, err := set.Capture(ctx, path)
			if err != nil {
				t.Error(err)
				return
			}
			if captured.SourcePath != path {
				t.Errorf("lost declaration: %s", captured.SourcePath)
			}
		})
	}
	group.Wait()
	if copies.Load() != 1 {
		t.Fatalf("physical copies=%d", copies.Load())
	}
}

func TestBoundedCaptureBoundaryAndOverflow(t *testing.T) {
	const limit = 64 << 20
	for _, size := range []int64{limit, limit + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			workspace := newTestWorkspace(t)
			source := filepath.Join(t.TempDir(), "font.ttf")
			file, err := os.Create(source)
			if err != nil {
				t.Fatal(err)
			}
			if err = file.Truncate(size); err != nil {
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			captured, err := NewSet(t.Context(), workspace).CaptureLimited(ctx, source, limit)
			if size == limit {
				if err != nil || captured.Size != limit {
					t.Fatalf("boundary: %+v %v", captured, err)
				}
			} else {
				var failure *LimitError
				if !errors.As(err, &failure) || scratchEntries(t, workspace) != 0 {
					t.Fatalf("overflow created content: %+v %v", captured, err)
				}
			}
		})
	}
}

func TestCaptureLimitedRejectsNonpositiveLimitsWithoutChangingSnapshots(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			workspace := newTestWorkspace(t)
			source := filepath.Join(t.TempDir(), "one-byte-source")
			if err := os.WriteFile(source, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			set := NewSet(t.Context(), workspace)
			if cached {
				if _, err := set.Capture(t.Context(), source); err != nil {
					t.Fatal(err)
				}
			}
			before := scratchEntries(t, workspace)
			for _, limit := range []int64{-1, 0} {
				captured, err := set.CaptureLimited(t.Context(), source, limit)
				if err == nil || captured != (Captured{}) || scratchEntries(t, workspace) != before {
					t.Fatalf("invalid limit %d returned content or changed snapshots: %+v, %v", limit, captured, err)
				}
			}
			captured, err := set.CaptureLimited(t.Context(), source, 1)
			if err != nil || captured.Size != 1 {
				t.Fatalf("positive one-byte boundary rejected: %+v, %v", captured, err)
			}
			content, err := os.ReadFile(captured.Path)
			if err != nil || string(content) != "x" || scratchEntries(t, workspace) != 1 {
				t.Fatalf("positive boundary lost actual snapshot bytes: %q, %v", content, err)
			}
		})
	}
}

func TestGrowingCaptureNeverWritesBeyondLimit(t *testing.T) {
	workspace := newTestWorkspace(t)
	source := filepath.Join(t.TempDir(), "growing.ttf")
	writeTestFile(t, source, make([]byte, copyChunkSize))
	var maximum int64
	workspace.afterChunk = func(size int64) {
		maximum = max(maximum, size)
		if err := os.WriteFile(source, make([]byte, copyChunkSize+1), 0o600); err != nil {
			t.Error(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	captured, err := NewSet(t.Context(), workspace).CaptureLimited(ctx, source, copyChunkSize)
	var failure *LimitError
	entries := scratchEntries(t, workspace)
	if !errors.As(err, &failure) || maximum > copyChunkSize || captured.Path != "" || entries != 0 {
		t.Fatalf("growth: max=%d captured=%+v scratch entries=%d err=%v", maximum, captured, entries, err)
	}
}
