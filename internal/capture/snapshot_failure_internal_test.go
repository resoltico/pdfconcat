// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotReportsOneChunkPerNonEmptyRead(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		size       int
		wantChunks int
	}{
		"empty file":                {0, 0},
		"one byte":                  {1, 1},
		"exactly one chunk":         {copyChunkSize, 1},
		"one byte over a chunk":     {copyChunkSize + 1, 2},
		"exactly two chunks":        {2 * copyChunkSize, 2},
		"one byte over two chunks":  {2*copyChunkSize + 1, 3},
		"one byte under a chunk":    {copyChunkSize - 1, 1},
		"one byte under two chunks": {2*copyChunkSize - 1, 2},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			workspace := newTestWorkspace(t)
			source := filepath.Join(t.TempDir(), inputName)
			writeTestFile(t, source, make([]byte, test.size))

			var sizes []int64

			workspace.afterChunk = func(copied int64) { sizes = append(sizes, copied) }

			captured, err := workspace.Snapshot(context.Background(), source)
			if err != nil || captured.Size != int64(test.size) {
				t.Fatalf("Snapshot() = %+v, %v", captured, err)
			}

			if len(sizes) != test.wantChunks {
				t.Fatalf("chunk callbacks %v, want %d of them", sizes, test.wantChunks)
			}

			for index := 1; index < len(sizes); index++ {
				if sizes[index] <= sizes[index-1] {
					t.Fatalf("a chunk was reported without bytes: %v", sizes)
				}
			}
		})
	}
}

func TestSnapshotOpenReportsAnUninspectableHandle(t *testing.T) {
	t.Parallel()

	workspace := newTestWorkspace(t)
	path := filepath.Join(t.TempDir(), inputName)
	writeTestFile(t, path, []byte("x"))

	source, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	err = source.Close()
	if err != nil {
		t.Fatal(err)
	}

	requireTestFileClosed(t, source)

	_, err = workspace.snapshotOpen(context.Background(), source, path)

	failure, isSource := errors.AsType[*SourceError](err)
	if !isSource || failure.Operation != "inspect source" || failure.Path != path || failure.Err == nil {
		t.Fatalf("snapshotOpen(closed handle) = %v", err)
	}

	if scratchEntries(t, workspace) != 0 {
		t.Fatal("scratch left behind")
	}
}

func TestVerifyUnchangedReportsAHandleThatCannotBeReinspected(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), inputName)
	writeTestFile(t, path, []byte("x"))

	source, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	before, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}

	err = source.Close()
	if err != nil {
		t.Fatal(err)
	}

	requireTestFileClosed(t, source)

	err = verifyUnchanged(source, before, &Captured{SourcePath: path, Size: before.Size()})

	failure, isSource := errors.AsType[*SourceError](err)
	if !isSource || failure.Operation != "re-inspect source" || failure.Path != path || failure.Err == nil {
		t.Fatalf("verifyUnchanged(closed handle) = %v", err)
	}
}

func TestVerifyUnchangedComparesEachObservation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), inputName)
	writeTestFile(t, path, []byte("four"))

	source, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	before, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}

	err = verifyUnchanged(source, before, &Captured{SourcePath: path, Size: before.Size()})
	if err != nil {
		t.Fatalf("an unchanged source: %v", err)
	}

	err = verifyUnchanged(source, before, &Captured{SourcePath: path, Size: before.Size() - 1})

	changed, isChanged := errors.AsType[*SourceChangedError](err)
	if !isChanged || changed.Path != path {
		t.Fatalf("fewer bytes read than the source holds: %v", err)
	}

	err = source.Close()
	if err != nil {
		t.Fatal(err)
	}
}
