// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type producedFailureCase struct {
	cause              error
	name               string
	content            string
	before             bool
	beforeWrite        bool
	writeFailure       bool
	ignoreWriteFailure bool
	cancelDuring       bool
	flushFailure       bool
}

const producedContent = "one"

func TestProducedStageWritesToOwnedFileBeforeProducerReturns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, reportPath)
	content := bytes.Repeat([]byte("x"), stageChunkSize+1)

	staged, err := StageProduced(t.Context(), target, int64(2*len(content)), func(w io.Writer) error {
		if _, err := w.Write(content); err != nil {
			return producedWriteError(err)
		}

		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("owned stage before completion: %v, %v", entries, err)
		}

		actual, err := os.ReadFile(filepath.Clean(filepath.Join(dir, entries[0].Name())))
		if err != nil || !bytes.Equal(actual, content) {
			t.Fatalf("producer bytes were buffered: %d bytes, %v", len(actual), err)
		}

		_, err = w.Write(content)

		return producedWriteError(err)
	})
	if err != nil {
		t.Fatal(err)
	}

	if staged.Size() != int64(2*len(content)) {
		t.Fatalf("size %d", staged.Size())
	}

	discardStaged(t, staged)
}

func TestProducedStageFailuresDiscardOwnedContent(t *testing.T) {
	t.Parallel()

	cases := []producedFailureCase{
		{name: "before write", beforeWrite: true, cause: errInjected},
		{name: "mid write", writeFailure: true, content: producedContent, cause: errInjected},
		{name: "limit", content: "four"},
		{name: "flush", flushFailure: true, content: producedContent, cause: errInjected},
		{name: "cancel before", before: true, cause: context.Canceled},
		{name: "cancel during", cancelDuring: true, content: producedContent, cause: context.Canceled},
		{name: "ignored write error", writeFailure: true, ignoreWriteFailure: true, content: producedContent, cause: errInjected},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) { t.Parallel(); checkProducedFailure(t, test) })
	}
}

func checkProducedFailure(t *testing.T, test producedFailureCase) {
	t.Helper()
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ops := producedFailureOperations(test, cancel)
	if test.before {
		cancel()
	}

	calls := 0

	_, err := stageProducedWith(ctx, ops, filepath.Join(dir, reportPath), 3, func(w io.Writer) error {
		calls++
		return produceFailureContent(w, test)
	})
	if err == nil || len(names(t, dir)) != 0 {
		t.Fatalf("failure %v; leftovers %v", err, names(t, dir))
	}

	if test.before && calls != 0 {
		t.Fatalf("canceled producer ran: %d calls", calls)
	}

	if test.cause != nil && !errors.Is(err, test.cause) {
		t.Fatalf("cause lost: %v", err)
	}

	if test.name == "limit" {
		limit, ok := errors.AsType[*SizeLimitError](err)
		if !ok || limit.Limit != 3 {
			t.Fatalf("limit: %v", err)
		}
	}
}

func producedFailureOperations(test producedFailureCase, cancel context.CancelFunc) operations {
	ops := realOperations()

	ops.writeChunk = func(dst io.Writer, chunk []byte) error {
		if test.writeFailure {
			return errInjected
		}

		err := writeChunk(dst, chunk)

		if test.cancelDuring {
			cancel()
		}

		return producedWriteError(err)
	}
	if test.flushFailure {
		ops.syncFile = func(string) error { return errInjected }
	}

	return ops
}

func produceFailureContent(w io.Writer, test producedFailureCase) error {
	if test.beforeWrite {
		return errInjected
	}

	_, err := io.WriteString(w, test.content)
	if test.ignoreWriteFailure {
		return nil
	}

	return producedWriteError(err)
}

func TestProducedStagePreservesCloseAndProductionFailures(t *testing.T) {
	t.Parallel()

	ops := realOperations()
	ops.writeChunk = func(dst io.Writer, _ []byte) error {
		file, ok := dst.(*os.File)
		if !ok {
			t.Fatalf("destination %T", dst)
		}

		if err := file.Close(); err != nil {
			t.Fatal(err)
		}

		return errInjected
	}

	_, err := stageProducedWith(t.Context(), ops, filepath.Join(t.TempDir(), reportPath), 100, func(w io.Writer) error {
		_, err := io.WriteString(w, producedContent)
		return producedWriteError(err)
	})
	if !errors.Is(err, errInjected) || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("production/close failure lost: %v", err)
	}
}

func producedWriteError(err error) error {
	if err != nil {
		return fmt.Errorf("produce staged content: %w", err)
	}

	return nil
}

func TestProducedWriterKeepsTheFirstFailureOnLaterWrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ops := realOperations()
	ops.writeChunk = func(io.Writer, []byte) error { return errInjected }

	_, err := stageProducedWith(t.Context(), ops, filepath.Join(dir, reportPath), 100, func(w io.Writer) error {
		_, firstErr := io.WriteString(w, producedContent)

		_, laterErr := io.WriteString(w, producedContent)
		if !errors.Is(firstErr, errInjected) || !errors.Is(laterErr, errInjected) {
			t.Fatalf("writer lost original failure: %v then %v", firstErr, laterErr)
		}

		return producedWriteError(laterErr)
	})
	if !errors.Is(err, errInjected) || len(names(t, dir)) != 0 {
		t.Fatalf("stage failure %v, leftovers %v", err, names(t, dir))
	}
}

func TestProducedWriterCancellationBeforeFirstWriteDiscardsTheStage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, err := StageProduced(ctx, filepath.Join(dir, reportPath), 100, func(w io.Writer) error {
		cancel()

		_, writeErr := io.WriteString(w, producedContent)
		if !errors.Is(writeErr, context.Canceled) {
			t.Fatalf("write cancellation lost: %v", writeErr)
		}

		return producedWriteError(writeErr)
	})
	if !errors.Is(err, context.Canceled) || len(names(t, dir)) != 0 {
		t.Fatalf("stage cancellation %v, leftovers %v", err, names(t, dir))
	}
}
