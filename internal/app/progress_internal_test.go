// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

type progressCapture struct {
	err     error
	entered chan struct{}
	release chan struct{}
	written chan struct{}
	records [][]byte
	once    sync.Once
	mutex   sync.Mutex
}

const progressTestAttempt = "attempt"

func (capture *progressCapture) WriteRecord(_ context.Context, data []byte) error {
	if capture.entered != nil {
		capture.once.Do(func() { close(capture.entered); <-capture.release })
	}

	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	capture.records = append(capture.records, bytes.Clone(data))
	if capture.written != nil {
		select {
		case capture.written <- struct{}{}:
		default:
		}
	}

	return capture.err
}

func (capture *progressCapture) recordsCopy() [][]byte {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()

	return append([][]byte(nil), capture.records...)
}

func TestProgressBlockedPresentationDoesNotBlockCallbacksOrLoseFinalPhases(t *testing.T) {
	t.Parallel()

	sink := &progressCapture{entered: make(chan struct{}), release: make(chan struct{})}
	session := NewProgress(t.Context(), cli.ProgressJSON, progressTestAttempt, sink)
	session.Observe(observation.Milestone{Phase: observation.Preparation})
	<-sink.entered

	finished := make(chan struct{})

	go func() {
		for value := range int64(100000) {
			session.Observe(
				observation.Milestone{
					Phase:      observation.InputInspection,
					Unit:       observation.ProcessedSources,
					Completed:  value,
					Total:      100000,
					TotalKnown: true,
				},
			)
		}

		session.Observe(
			observation.Milestone{
				Phase:      observation.Layout,
				Unit:       observation.ProcessedGeneratedSpecs,
				Completed:  2,
				Total:      3,
				TotalKnown: true,
			},
		)
		session.Observe(observation.Milestone{Phase: observation.Finalization})
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("worker callback waited on blocked sink")
	}

	close(sink.release)
	session.Stop()

	records := requireFinalProgressPhases(t, sink)
	before := len(records)

	session.Observe(observation.Milestone{Phase: observation.Finalization, Advance: true})
	session.Stop()

	if len(sink.recordsCopy()) != before {
		t.Fatal("record emitted after stop/join")
	}
}

func requireFinalProgressPhases(t *testing.T, sink *progressCapture) []progressRecord {
	t.Helper()
	records := decodeProgressRecords(t, sink.recordsCopy())
	phases := []string{}
	seenFinal := false

	for index := range records {
		record := &records[index]
		if record.Event == "phase" {
			phases = append(phases, record.Phase)
		}

		if record.Unit == observation.ProcessedSources.String() && record.Completed != nil && *record.Completed == 99999 {
			seenFinal = true
		}
	}

	if !seenFinal {
		t.Fatal("final observed count disappeared at phase transition")
	}

	if got := strings.Join(phases, ","); got != "preparation,input_inspection,layout,finalization" {
		t.Fatalf("phase order %s", got)
	}

	return records
}

func TestProgressStateRejectsStaleCountsAndKeepsIndependentWorkClock(t *testing.T) {
	t.Parallel()

	began := time.Unix(1, 0)
	state := progressState{began: began}
	state.observe(observation.Milestone{Phase: observation.InputInspection}, began)

	if !state.lastAdvance.IsZero() {
		t.Fatal("phase entry invented work advancement")
	}

	count := observation.Milestone{
		Phase:      observation.InputInspection,
		Unit:       observation.ProcessedSources,
		Completed:  2,
		Total:      3,
		TotalKnown: true,
	}
	state.observe(count, began.Add(time.Second))
	count.Completed = 1
	state.observe(count, began.Add(2*time.Second))
	count.Completed = 2
	state.observe(count, began.Add(3*time.Second))

	if !state.lastAdvance.Equal(began.Add(time.Second)) {
		t.Fatal("duplicate/stale count reset work age")
	}

	state.observe(observation.Milestone{Phase: observation.Layout}, began.Add(4*time.Second))

	count.Completed = 3
	state.observe(count, began.Add(5*time.Second))

	if state.active != observation.Layout ||
		state.phases[observation.InputInspection].counters[observation.ProcessedSources].completed != 2 {
		t.Fatal("old joined phase mutated newer work")
	}
}

func TestProgressHeartbeatDoesNotAdvanceWorkAndCountersOutlive600Updates(t *testing.T) {
	t.Parallel()

	sink := &progressCapture{}
	began := time.Unix(1, 0)
	state := progressState{began: began}
	state.observe(observation.Milestone{Phase: observation.Assembly}, began)
	now := began
	controller := &Progress{mode: cli.ProgressJSON, attempt: progressTestAttempt, writer: sink, now: func() time.Time { return now }}

	emitted := progressEmission{}
	if !emitted.drain(t.Context(), controller, &state) {
		t.Fatal("phase emit failed")
	}

	now = now.Add(progressHeartbeatInterval)

	emitted.drain(t.Context(), controller, &state)
	requireUnadvancedWorkClock(t, controller, &state)

	for index := int64(1); index <= 700; index++ {
		now = now.Add(minRedrawInterval)
		state.observe(
			observation.Milestone{
				Phase:      observation.Assembly,
				Unit:       observation.CompiledPages,
				Completed:  index,
				Total:      700,
				TotalKnown: true,
			},
			now,
		)
		emitted.drain(t.Context(), controller, &state)
	}

	now = now.Add(progressHeartbeatInterval)

	emitted.drain(t.Context(), controller, &state)

	records := decodeProgressRecords(t, sink.recordsCopy())
	if records[1].Event != "liveness" || records[1].WorkAgeMS != nil {
		t.Fatal("pre-work heartbeat invented an advance")
	}

	last := records[len(records)-1]
	if last.Event != "liveness" || last.WorkAgeMS == nil || *last.WorkAgeMS != progressHeartbeatInterval.Milliseconds() {
		t.Fatal("heartbeat reset actual work age")
	}

	if len(records) < 703 {
		t.Fatal("whole-run redraw cutoff survived")
	}

	for index, record := range records {
		if record.Sequence != uint64(index+1) {
			t.Fatal("sequence was not monotonic")
		}
	}
}

func TestProgressHumanPadsAndErasesWithoutTerminalEscapes(t *testing.T) {
	t.Parallel()

	sink := &progressCapture{}
	now := time.Unix(1, 0)
	controller := &Progress{writer: sink, mode: cli.ProgressAuto, now: func() time.Time { return now }}
	state := progressState{began: now}
	state.observe(
		observation.Milestone{
			Phase:      observation.InputInspection,
			Unit:       observation.ProcessedSources,
			Completed:  1,
			Total:      2,
			TotalKnown: true,
		},
		now,
	)

	emitted := progressEmission{}
	emitted.drain(t.Context(), controller, &state)
	state.observe(observation.Milestone{Phase: observation.Layout}, now)
	emitted.drain(t.Context(), controller, &state)
	emitted.erase(t.Context(), controller)

	data := bytes.Join(sink.recordsCopy(), nil)
	if bytes.Contains(data, []byte("\x1b")) || !bytes.HasSuffix(data, []byte("\r")) {
		t.Fatal("human progress used escapes or left line uncleared")
	}

	if !bytes.Contains(data, []byte("1/2 processed sources")) {
		t.Fatal("human count lacks truthful unit")
	}
}

func TestProgressObserverUsesConcurrentMonotonicState(t *testing.T) {
	t.Parallel()

	var clock atomic.Int64

	controller := &Progress{wake: make(chan struct{}, 1), now: func() time.Time { return time.Unix(0, clock.Load()) }}
	controller.state.began = controller.now()

	var group sync.WaitGroup
	for value := int64(100); value >= 1; value-- {
		group.Go(func() {
			controller.Observe(
				observation.Milestone{
					Phase:      observation.InputInspection,
					Unit:       observation.ProcessedSources,
					Completed:  value,
					Total:      100,
					TotalKnown: true,
				},
			)
		})
	}

	group.Wait()

	if controller.snapshot().phases[observation.InputInspection].counters[observation.ProcessedSources].completed != 100 {
		t.Fatal("reversed concurrent callbacks regressed counter")
	}
}

func decodeProgressRecords(t *testing.T, data [][]byte) []progressRecord {
	t.Helper()

	records := make([]progressRecord, len(data))
	for index, record := range data {
		if len(record) == 0 || record[len(record)-1] != '\n' {
			t.Fatal("machine record missing LF")
		}

		if err := json.Unmarshal(record, &records[index]); err != nil {
			t.Fatal(err)
		}
	}

	return records
}

func TestProgressFailureDuringFinalDrainHasNoFollowingErase(t *testing.T) {
	t.Parallel()

	sink := &progressCapture{err: io.ErrClosedPipe}
	session := startProgress(t.Context(), cli.ProgressAuto, progressTestAttempt, sink, time.Now, nil, func() {})
	session.Observe(observation.Milestone{Phase: observation.Preparation})
	session.Stop()

	if !session.Interrupted() || len(sink.recordsCopy()) != 1 {
		t.Fatal("failed final record followed by erase or failure hidden")
	}
}

func requireUnadvancedWorkClock(t *testing.T, controller *Progress, state *progressState) {
	t.Helper()

	if !controller.snapshot().lastAdvance.IsZero() || !state.lastAdvance.IsZero() {
		t.Fatal("heartbeat changed producer work clock")
	}
}
