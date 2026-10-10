// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package capture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
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
	set := NewSet(workspace)
	var group sync.WaitGroup
	for index := range 40 {
		group.Go(func() {
			path := source
			if index%2 != 0 {
				path = alias
			}
			captured, err := set.Capture(t.Context(), path)
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
			captured, err := NewSet(workspace).CaptureLimited(t.Context(), source, limit)
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
func TestGrowingCaptureNeverWritesBeyondLimit(t *testing.T) {
	workspace := newTestWorkspace(t)
	source := filepath.Join(t.TempDir(), "growing.ttf")
	writeTestFile(t, source, make([]byte, copyChunkSize))
	var maximum int64
	workspace.afterChunk = func(size int64) {
		maximum = max(maximum, size)
		if err := os.WriteFile(source, make([]byte, copyChunkSize+1), 0600); err != nil {
			t.Error(err)
		}
	}
	captured, err := NewSet(workspace).CaptureLimited(t.Context(), source, copyChunkSize)
	var failure *LimitError
	if !errors.As(err, &failure) || maximum > copyChunkSize || captured.Path != "" || scratchEntries(t, workspace) != 0 {
		t.Fatalf("growth: max=%d captured=%+v err=%v", maximum, captured, err)
	}
}
