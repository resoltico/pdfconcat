// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type (
	// endingContext is a context that never signals Done but reports the failure the reader sets.
	endingContext struct {
		failure error
		mu      sync.Mutex
	}

	// endingReader supplies one byte and ends its context at that moment, so the copy meets the ended
	// context between two chunks.
	endingReader struct {
		ctx   *endingContext
		cause error
		done  bool
	}
)

func (*endingContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (*endingContext) Done() <-chan struct{} { return nil }

func (*endingContext) Value(any) any { return nil }

func (c *endingContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.failure
}

func (r *endingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}

	r.done = true

	r.ctx.mu.Lock()
	r.ctx.failure = r.cause
	r.ctx.mu.Unlock()

	p[0] = 'x'

	return 1, nil
}

func TestStageEndedContextIsReportedAsTheContextErrorNotAsAWriteFailure(t *testing.T) {
	t.Parallel()

	for name, cause := range map[string]error{canceledAction: context.Canceled, "deadline": context.DeadlineExceeded} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			ctx := &endingContext{}

			_, err := Stage(ctx, filepath.Join(dir, shortReportPath), &endingReader{ctx: ctx, cause: cause}, 1<<20)

			stageErr, isStageError := errors.AsType[*StageError](err)
			if !errors.Is(err, cause) || isStageError {
				t.Fatalf("Stage() = %v (stage error %v)", err, stageErr)
			}

			if len(names(t, dir)) != 0 {
				t.Fatalf(stagingLeftFormat, names(t, dir))
			}
		})
	}
}

func TestStageMeasuresTheWholeContentAgainstTheLimit(t *testing.T) {
	t.Parallel()

	content := bytes.Repeat([]byte("0"), 2*stageChunkSize+7)
	total := int64(len(content))

	tests := map[string]struct {
		limit   int64
		wantErr bool
	}{
		"limit equals the content":           {total, false},
		"limit one byte short":               {total - 1, true},
		"limit just above one chunk":         {stageChunkSize + 10, true},
		"limit above the content":            {total + 1, false},
		"limit equals the first chunk":       {stageChunkSize, true},
		"limit equals the first two chunks":  {2 * stageChunkSize, true},
		"limit one below the last-chunk end": {total - 2, true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if test.wantErr {
				requireLimitExceeded(t, content, test.limit)
			} else {
				requireStagedExactly(t, content, test.limit)
			}
		})
	}
}

// requireLimitExceeded stages content under limit and expects the limit error naming the target and no
// leftover file.
func requireLimitExceeded(t *testing.T, content []byte, limit int64) {
	t.Helper()

	dir := t.TempDir()
	target := filepath.Join(dir, shortReportPath)

	_, err := Stage(t.Context(), target, bytes.NewReader(content), limit)

	tooLarge, isLimit := errors.AsType[*SizeLimitError](err)
	if !isLimit || tooLarge.Limit != limit || tooLarge.Target != target || len(names(t, dir)) != 0 {
		t.Fatalf("Stage() = %v; directory %v", err, names(t, dir))
	}
}

// requireStagedExactly stages content under limit and expects a staged file holding all of it.
func requireStagedExactly(t *testing.T, content []byte, limit int64) {
	t.Helper()

	total := int64(len(content))

	staged, err := Stage(t.Context(), filepath.Join(t.TempDir(), shortReportPath), bytes.NewReader(content), limit)
	if err != nil {
		t.Fatal(err)
	}

	info, statErr := os.Stat(staged.Path())
	if statErr != nil || staged.Size() != total || info.Size() != total {
		t.Fatalf("staged size %d, file %v (%v), want %d", staged.Size(), info, statErr, total)
	}

	discardStaged(t, staged)
}

func TestStageWritesOnlyNonEmptyChunks(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		size       int
		wantWrites int
	}{
		"empty content":          {0, 0},
		"one byte":               {1, 1},
		"exactly one chunk":      {stageChunkSize, 1},
		"one byte over a chunk":  {stageChunkSize + 1, 2},
		"exactly two chunks":     {2 * stageChunkSize, 2},
		"one byte under a chunk": {stageChunkSize - 1, 1},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var written []int

			ops := realOperations()
			ops.writeChunk = func(dst io.Writer, chunk []byte) error {
				written = append(written, len(chunk))

				return writeChunk(dst, chunk)
			}

			target := filepath.Join(t.TempDir(), shortReportPath)

			staged, err := stageWith(t.Context(), ops, target, bytes.NewReader(make([]byte, test.size)), 1<<30)
			if err != nil {
				t.Fatal(err)
			}

			discardStaged(t, staged)

			if len(written) != test.wantWrites {
				t.Fatalf("chunk sizes written: %v, want %d writes", written, test.wantWrites)
			}

			for _, size := range written {
				if size == 0 {
					t.Fatalf("an empty chunk was written: %v", written)
				}
			}
		})
	}
}

func TestDiscardAfterPublicationNeverRemovesWhatIsAtTheStagedPath(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		flushDirectory func(string) error
		wantErr        bool
	}{
		"clean publication":                {func(string) error { return nil }, false},
		"publication with a flush failure": {func(string) error { return errInjected }, true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ops := realOperations()
			ops.syncDirectory = test.flushDirectory

			dir := t.TempDir()
			staged := stageText(t, ops, filepath.Join(dir, outputReportPath), stagedContent)
			path := staged.Path()

			renamed, err := staged.publishRetainingContext(t.Context(), Policy{})
			if (err != nil) != test.wantErr || renamed != test.wantErr {
				t.Fatalf("publishRetainingContext() = %v, %v", renamed, err)
			}

			// The staged name is free again once published; whatever appears there now is not ours to remove.
			put(t, path, "someone else's file")

			err = staged.Discard()
			if err != nil || get(t, path) != "someone else's file" {
				t.Fatalf("Discard() = %v; the file at the staged path is %q", err, getIfPresent(t, path))
			}
		})
	}
}

func TestDiscardAfterAFailedPublicationRemovesTheStagedFile(t *testing.T) {
	t.Parallel()

	ops := realOperations()
	ops.replace = func(string, string, existingFile) error { return errInjected }

	dir := t.TempDir()
	staged := stageText(t, ops, filepath.Join(dir, outputReportPath), stagedContent)

	_, err := staged.publishRetainingContext(t.Context(), Policy{})
	if !errors.Is(err, errInjected) {
		t.Fatalf("publishRetainingContext() = %v", err)
	}

	err = staged.Discard()
	if err != nil || len(names(t, dir)) != 0 {
		t.Fatalf("Discard() = %v; directory %v", err, names(t, dir))
	}
}
