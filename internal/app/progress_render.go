// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	progressRecord struct {
		Completed     *int64 `json:"completed"`
		Total         *int64 `json:"total"`
		WorkAgeMS     *int64 `json:"work_age_ms"`
		AttemptID     string `json:"attempt_id"`
		Kind          string `json:"kind"`
		Event         string `json:"event"`
		Phase         string `json:"phase"`
		Unit          string `json:"unit,omitempty"`
		Sequence      uint64 `json:"sequence"`
		ElapsedMS     int64  `json:"elapsed_ms"`
		PhaseAgeMS    int64  `json:"phase_age_ms"`
		FormatVersion int    `json:"format_version"`
	}
	progressEmission struct {
		lastCounter   time.Time
		lastEmission  time.Time
		lastHeartbeat time.Time
		counters      [observation.PhaseCount][observation.UnitCount]progressCounter
		humanWidth    int
		phases        [observation.PhaseCount]bool
	}
)

func (controller *Progress) run(ctx context.Context) {
	defer close(controller.joined)

	emitted := progressEmission{}

	for {
		select {
		case <-controller.wake:
		case <-controller.ticks:
		case <-controller.stop:
			state := controller.snapshot()
			if emitted.drain(ctx, controller, &state) {
				emitted.erase(ctx, controller)
			}

			return
		}

		state := controller.snapshot()
		if !emitted.drain(ctx, controller, &state) {
			<-controller.stop
			return
		}
	}
}

func (emitted *progressEmission) drain(ctx context.Context, controller *Progress, state *progressState) bool {
	if !state.hasPhase {
		return true
	}

	now := controller.now()
	wroteCounters := false

	for phase := observation.Preparation; phase <= state.active; phase++ {
		if !state.phases[phase].entered {
			continue
		}

		if !emitted.phases[phase] {
			if !emitted.record(ctx, controller, state, phase, observation.UnitNone, "phase", now) {
				return false
			}

			emitted.phases[phase] = true
		}

		wrote, ok := emitted.phaseCounters(ctx, controller, state, phase, now)
		if !ok {
			return false
		}

		wroteCounters = wroteCounters || wrote
	}

	if wroteCounters {
		emitted.lastCounter = now
	}

	return emitted.heartbeat(ctx, controller, state, now)
}

func (emitted *progressEmission) phaseCounters(
	ctx context.Context,
	controller *Progress,
	state *progressState,
	phase observation.Phase,
	now time.Time,
) (bool, bool) {
	due := phase < state.active || state.stopping || now.Sub(emitted.lastCounter) >= minRedrawInterval
	if !due {
		return false, true
	}

	wrote := false

	for unit := observation.UnitNone + 1; unit < observation.UnitCount; unit++ {
		counter := state.phases[phase].counters[unit]
		if !counter.observed || counter == emitted.counters[phase][unit] {
			continue
		}

		if !emitted.record(ctx, controller, state, phase, unit, "counter", now) {
			return wrote, false
		}

		emitted.counters[phase][unit] = counter
		wrote = true
	}

	return wrote, true
}

func (emitted *progressEmission) heartbeat(ctx context.Context, controller *Progress, state *progressState, now time.Time) bool {
	if controller.mode != cli.ProgressJSON || state.stopping {
		return true
	}

	if now.Sub(emitted.lastEmission) < progressHeartbeatInterval || now.Sub(emitted.lastHeartbeat) < progressHeartbeatInterval {
		return true
	}

	if !emitted.record(ctx, controller, state, state.active, observation.UnitNone, "liveness", now) {
		return false
	}

	emitted.lastHeartbeat = now

	return true
}

func (emitted *progressEmission) record(
	ctx context.Context,
	controller *Progress,
	state *progressState,
	phase observation.Phase,
	unit observation.Unit,
	event string,
	now time.Time,
) bool {
	var data []byte
	if controller.mode == cli.ProgressJSON {
		data = progressJSONRecord(controller, state, phase, unit, event, now)
	} else {
		text := progressHumanLine(state, phase)
		padding := max(emitted.humanWidth-len(text), 0)
		data = []byte("\r" + text + strings.Repeat(" ", padding) + "\r" + text)
		emitted.humanWidth = max(emitted.humanWidth, len(text))
	}

	emitted.lastEmission = now

	writeErr := controller.writer.WriteRecord(ctx, data)
	if writeErr != nil && ctx.Err() == nil {
		controller.interrupted.Store(true)
	}

	return writeErr == nil
}

func (emitted *progressEmission) erase(ctx context.Context, controller *Progress) {
	if controller.mode == cli.ProgressJSON || emitted.humanWidth == 0 {
		return
	}

	writeErr := controller.writer.WriteRecord(ctx, []byte("\r"+strings.Repeat(" ", emitted.humanWidth)+"\r"))
	if writeErr != nil && ctx.Err() == nil {
		controller.interrupted.Store(true)
	}
}

func progressHumanLine(state *progressState, phase observation.Phase) string {
	text := "pdfconcat: " + strings.ReplaceAll(phase.String(), "_", " ")

	var counterText strings.Builder

	for unit := observation.UnitNone + 1; unit < observation.UnitCount; unit++ {
		counter := state.phases[phase].counters[unit]
		if !counter.observed {
			continue
		}

		count := strconv.FormatInt(counter.completed, 10)
		if counter.known {
			count = fmt.Sprintf("%d/%d", counter.completed, counter.total)
		}

		counterText.WriteString(" " + count + " " + strings.ReplaceAll(unit.String(), "_", " "))
	}

	text += counterText.String()

	return text
}

func progressJSONRecord(
	controller *Progress,
	state *progressState,
	phase observation.Phase,
	unit observation.Unit,
	event string,
	now time.Time,
) []byte {
	record := progressRecord{
		FormatVersion: report.Version,
		Kind:          "progress",
		Event:         event,
		AttemptID:     controller.attempt,
		Sequence:      controller.NextSequence(),
		Phase:         phase.String(),
		ElapsedMS:     now.Sub(state.began).Milliseconds(),
		PhaseAgeMS:    now.Sub(state.phases[phase].began).Milliseconds(),
	}
	if !state.lastAdvance.IsZero() {
		age := now.Sub(state.lastAdvance).Milliseconds()
		record.WorkAgeMS = &age
	}

	if unit != observation.UnitNone {
		counter := state.phases[phase].counters[unit]
		record.Unit = unit.String()

		record.Completed = &counter.completed
		if counter.known {
			record.Total = &counter.total
		}
	}

	encoded, _ := json.Marshal(record)

	encoded = append(encoded, '\n')

	return encoded
}
