// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
)

func TestHumanWarningRoutesOnlyWithBothProgressCapabilities(t *testing.T) {
	t.Parallel()
	for _, recordAvailable := range []bool{true, false} {
		for _, sequenceAvailable := range []bool{false, true} {
			var stderr bytes.Buffer
			sink := &progressExceptionCapture{err: errInjected}
			sequenceCalls := 0
			env := Env{Stderr: &stderr}
			if recordAvailable {
				env.ProgressRecord = sink
			}
			if sequenceAvailable {
				env.ProgressSequence = func() uint64 { sequenceCalls++; return 1 }
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			err := warn(ctx, env, &cli.Command{Progress: cli.ProgressAuto}, nil, "cleanup failed")
			want := binaryName + ": warning: cleanup failed\n"
			if recordAvailable && sequenceAvailable {
				if !errors.Is(err, errInjected) || string(sink.record) != want || stderr.Len() != 0 || sink.operationErr != nil || sink.hasDone {
					t.Fatalf("owned warning delivery: err=%v record=%q stderr=%q", err, sink.record, stderr.String())
				}
			} else if err != nil || stderr.String() != want || len(sink.record) != 0 {
				t.Fatalf("warning fallback (record=%t sequence=%t): err=%v record=%q stderr=%q", recordAvailable, sequenceAvailable, err, sink.record, stderr.String())
			}
			if sequenceCalls != 0 {
				t.Fatal("human warning consumed a telemetry sequence")
			}
		}
	}
}
