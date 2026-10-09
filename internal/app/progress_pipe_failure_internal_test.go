// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

type progressPipeFailure struct {
	file   *os.File
	result chan error
}

func (sink *progressPipeFailure) WriteRecord(_ context.Context, record []byte) error {
	_, err := sink.file.Write(record)
	sink.result <- err

	if err != nil {
		return fmt.Errorf("write progress fixture pipe: %w", err)
	}

	return nil
}

func TestProgressPresentationJoinsAfterRealPipeFailure(t *testing.T) {
	t.Parallel()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := writer.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	sink := &progressPipeFailure{file: writer, result: make(chan error, 1)}

	session := NewProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, sink)
	defer session.Stop()

	session.Observe(observation.Milestone{Phase: observation.Preparation})

	select {
	case writeErr := <-sink.result:
		if writeErr == nil {
			t.Fatal("closed-reader pipe accepted a real progress write")
		}

		t.Logf("actual closed-reader pipe write: %v", writeErr)
	case <-time.After(time.Second):
		t.Fatal("presentation did not attempt the broken native pipe")
	}

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticks := time.NewTicker(time.Millisecond)
	defer ticks.Stop()

	for !session.Interrupted() {
		select {
		case <-deadline.C:
			t.Fatal("actual presentation failure was not observed before Stop")
		case <-ticks.C:
		}
	}

	session.Stop()

	select {
	case <-session.joined:
	default:
		t.Fatal("failed presentation was not joined")
	}
}
