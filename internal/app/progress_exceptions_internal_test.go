// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	progressExceptionContextKey struct{}
	progressExceptionCapture    struct {
		operationValue any
		operationErr   error
		err            error
		record         []byte
		hasDeadline    bool
		hasDone        bool
	}
)

func (capture *progressExceptionCapture) WriteRecord(ctx context.Context, record []byte) error {
	capture.operationValue = ctx.Value(progressExceptionContextKey{})
	capture.operationErr = ctx.Err()
	_, capture.hasDeadline = ctx.Deadline()
	capture.hasDone = ctx.Done() != nil
	capture.record = bytes.Clone(record)

	return capture.err
}

func TestCleanupProgressRequiresActualRecordChannelAndSequence(t *testing.T) {
	t.Parallel()

	sink := &progressExceptionCapture{}
	sequenceCalls := 0

	sequence := func() uint64 { sequenceCalls++; return 1 }
	for _, env := range []Env{{}, {ProgressRecord: sink}, {ProgressSequence: sequence}} {
		// A quiet/unavailable channel must not need a report or invent a sequence.
		if err := cleanupOnStderr(t.Context(), env, nil, "cleanup warning"); err != nil {
			t.Fatal(err)
		}
	}

	if sink.record != nil || sequenceCalls != 0 {
		t.Fatal("unavailable exception channel consumed sequence or wrote a record")
	}

	if err := writeProgressException(t.Context(), Env{}, struct{}{}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupProgressRetainsExactFactsAfterOperationCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), progressExceptionContextKey{}, "operation"))
	cancel()

	sink := &progressExceptionCapture{err: errInjected}
	rep := report.NewBuilder(buildName).Build(report.StatusFailed)
	rep.Publication = report.Publication{Published: true, Output: "/" + strings.Repeat("ē", 1500) + "/out.pdf"}
	message := "cleanup: " + strings.Repeat("long/🙂/path/", 1000)

	env := Env{ProgressRecord: sink, ProgressSequence: func() uint64 { return 42 }}
	if err := cleanupOnStderr(ctx, env, rep, message); !errors.Is(err, errInjected) {
		t.Fatalf("exception transport failure hidden: %v", err)
	}

	if sink.operationErr != nil || sink.operationValue != "operation" {
		t.Fatal("bounded exception lost operation values or inherited producer cancellation")
	}

	if sink.hasDeadline || sink.hasDone {
		t.Fatal("producer cancellation remained attached to final exception")
	}

	assertProgressCleanupFacts(t, sink.record, rep, message)
}

func assertProgressCleanupFacts(t *testing.T, encoded []byte, rep *report.Report, message string) {
	t.Helper()

	var record cleanupRecord
	if err := json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}

	if record.Message != message || record.Publication.Output != rep.Publication.Output || record.Sequence != 42 ||
		record.AttemptID != rep.AttemptID || record.SavedRun != report.SavedRunOf(rep) || record.Kind != "cleanup_warning" {
		t.Fatal("machine cleanup exception trimmed or invented captured facts")
	}

	if !bytes.HasSuffix(encoded, []byte{'\n'}) || record.FormatVersion != report.Version {
		t.Fatal("cleanup exception is not a current full JSON+LF record")
	}
}
