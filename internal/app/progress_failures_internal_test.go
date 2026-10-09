// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"io"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

const progressFailureHeartbeat = "liveness"

func TestProgressUnselectedOrIdleSessionInventsNoObservations(t *testing.T) {
	t.Parallel()

	sink := &progressCapture{}
	if NewProgress(t.Context(), cli.ProgressNone, progressTestAttempt, sink) != nil {
		t.Fatal("none created presentation")
	}

	if NewProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, nil) != nil {
		t.Fatal("nil sink created presentation")
	}

	session := NewProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, sink)
	session.Stop()

	if len(sink.recordsCopy()) != 0 {
		t.Fatal("idle Stop invented phase/work observations")
	}
}

func TestProgressCounterAndHeartbeatFailuresEndPresentation(t *testing.T) {
	t.Parallel()

	for _, event := range []string{"counter", progressFailureHeartbeat} {
		t.Run(event, func(t *testing.T) {
			t.Parallel()

			began := time.Unix(1, 0)
			now := began
			sink := &progressCapture{}
			controller := &Progress{
				mode:    cli.ProgressJSON,
				attempt: progressTestAttempt,
				writer:  sink,
				now:     func() time.Time { return now },
			}
			state := progressState{began: began}
			state.observe(observation.Milestone{Phase: observation.Assembly}, began)

			emission := progressEmission{}
			if !emission.drain(t.Context(), controller, &state) {
				t.Fatal("healthy initial phase failed")
			}

			if event == "counter" {
				state.observe(observation.Milestone{Phase: observation.Assembly, Unit: observation.CompiledPages, Completed: 1}, began)
			}

			now = began.Add(progressHeartbeatInterval)
			sink.err = io.ErrClosedPipe

			if emission.drain(t.Context(), controller, &state) || !controller.Interrupted() {
				t.Fatal("optional presentation failure hidden")
			}
		})
	}
}

func TestProgressHumanEraseFailureIsVisible(t *testing.T) {
	t.Parallel()

	sink := &progressCapture{err: io.ErrClosedPipe}
	controller := &Progress{mode: cli.ProgressAuto, writer: sink}
	emission := progressEmission{humanWidth: 10}
	emission.erase(t.Context(), controller)

	if !controller.Interrupted() || len(sink.recordsCopy()) != 1 {
		t.Fatal("erase failure hidden or repeated")
	}
}
