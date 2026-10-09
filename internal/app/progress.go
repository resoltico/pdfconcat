// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
)

type (
	// RecordWriter is the process-owned bounded native record transport.
	// App observations do not call it; only the joined presentation worker does.
	RecordWriter interface {
		WriteRecord(ctx context.Context, record []byte) error
	}

	// Progress owns bounded scheduling and presentation for one attempt.
	Progress struct {
		writer      RecordWriter
		joined      chan struct{}
		stopTicker  func()
		ticks       <-chan time.Time
		wake        chan struct{}
		stop        chan struct{}
		now         func() time.Time
		attempt     string
		mode        cli.ProgressMode
		state       progressState
		sequence    atomic.Uint64
		stopOnce    sync.Once
		mutex       sync.Mutex
		interrupted atomic.Bool
	}
	progressCounter struct {
		completed int64
		total     int64
		known     bool
		observed  bool
	}
	progressPhase struct {
		began    time.Time
		counters [observation.UnitCount]progressCounter
		entered  bool
	}
	progressState struct {
		began       time.Time
		lastAdvance time.Time
		phases      [observation.PhaseCount]progressPhase
		active      observation.Phase
		hasPhase    bool
		stopping    bool
	}
)

const (
	stageRender               = "render"
	minRedrawInterval         = 100 * time.Millisecond
	progressHeartbeatInterval = 10 * time.Second
)

// NewProgress starts optional bounded presentation for an already selected sink.
// Main owns sink selection, native construction and final sink close.
func NewProgress(ctx context.Context, mode cli.ProgressMode, attempt string, writer RecordWriter) *Progress {
	if writer == nil || mode == cli.ProgressNone {
		return nil
	}

	ticker := time.NewTicker(minRedrawInterval)

	return startProgress(ctx, mode, attempt, writer, time.Now, ticker.C, ticker.Stop)
}

func startProgress(
	ctx context.Context,
	mode cli.ProgressMode,
	attempt string,
	writer RecordWriter,
	now func() time.Time,
	ticks <-chan time.Time,
	stopTicker func(),
) *Progress {
	controller := &Progress{
		mode: mode, attempt: attempt, writer: writer, now: now, ticks: ticks, stopTicker: stopTicker,
		wake: make(chan struct{}, 1), stop: make(chan struct{}), joined: make(chan struct{}),
		state: progressState{began: now()},
	}
	go controller.run(ctx)

	return controller
}

// Observe only merges bounded typed state. It never encodes or waits for I/O.
func (controller *Progress) Observe(milestone observation.Milestone) {
	controller.mutex.Lock()
	changed := controller.state.observe(milestone, controller.now())
	controller.mutex.Unlock()

	if changed {
		select {
		case controller.wake <- struct{}{}:
		default:
		}
	}
}

// Stop drains retained final observations and joins the sole presenter.
func (controller *Progress) Stop() {
	controller.stopOnce.Do(func() {
		controller.mutex.Lock()
		controller.state.stopping = true
		controller.mutex.Unlock()
		close(controller.stop)
		<-controller.joined
		controller.stopTicker()
	})
}

// NextSequence shares ordering with exceptional records after Stop.
func (controller *Progress) NextSequence() uint64 { return controller.sequence.Add(1) }

// Interrupted reports an actual presentation failure, excluding unsent canceled updates.
func (controller *Progress) Interrupted() bool { return controller.interrupted.Load() }

func (state *progressState) observe(milestone observation.Milestone, now time.Time) bool {
	if !state.accepts(milestone) {
		return false
	}

	phase := &state.phases[milestone.Phase]

	changed := state.enter(phase, milestone.Phase, now)
	if milestone.Unit == observation.UnitNone {
		if milestone.Advance {
			state.lastAdvance = now
			return true
		}

		return changed
	}

	advanced, updated := phase.counters[milestone.Unit].merge(milestone)
	if advanced {
		state.lastAdvance = now
	}

	return changed || updated
}

func (state *progressState) accepts(milestone observation.Milestone) bool {
	if state.stopping || milestone.Phase >= observation.PhaseCount || milestone.Unit >= observation.UnitCount {
		return false
	}

	if state.hasPhase && milestone.Phase < state.active {
		return false
	}

	return milestone.Completed >= 0 && (!milestone.TotalKnown || milestone.Total >= milestone.Completed)
}

func (state *progressState) enter(phase *progressPhase, rank observation.Phase, now time.Time) bool {
	if phase.entered {
		return false
	}

	phase.entered = true
	phase.began = now
	state.active = rank
	state.hasPhase = true

	return true
}

func (counter *progressCounter) merge(milestone observation.Milestone) (bool, bool) {
	advanced := milestone.Completed > counter.completed

	changed := !counter.observed || advanced
	if changed {
		counter.completed = milestone.Completed
		counter.observed = true
	}

	if milestone.TotalKnown && !counter.known {
		counter.total = milestone.Total
		counter.known = true
		changed = true
	}

	return advanced, changed
}

func (controller *Progress) snapshot() progressState {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()

	return controller.state
}
