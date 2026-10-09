// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/observation"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

type phaseRecordSink struct {
	failure error
	records [][]byte
	mutex   sync.Mutex
}

const (
	progressJSONFlag = "--progress=json"
	progressReceipt  = "receipt.json"
)

func (sink *phaseRecordSink) WriteRecord(_ context.Context, record []byte) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()

	sink.records = append(sink.records, bytes.Clone(record))

	return sink.failure
}

func TestCheckAndBuildProgressFollowRealPhasePaths(t *testing.T) {
	t.Parallel()

	for _, command := range []string{commandCheck, commandBuild} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			dir := workDir(t)
			writePDF(t, dir, sourceA)

			sink := &phaseRecordSink{}

			var session *app.Progress

			env := app.Env{WorkingDir: dir, NewProgress: func(ctx context.Context, mode cli.ProgressMode, attempt string) *app.Progress {
				session = app.NewProgress(ctx, mode, attempt, sink)
				return session
			}}

			args := []string{command, sourceA, blankFlag, progressJSONFlag, reportFlag, progressReceipt}
			if command == commandBuild {
				args = append(args, "-o", outputFile)
			}

			result := executeWith(t.Context(), t, appOf(newFake(t)), env, args...)
			result.requireCode(t, 0, "")
			phases := phaseNamesFromRecords(t, sink.records)
			requirePhase(t, phases, "preparation")
			requirePhase(t, phases, "input_inspection")
			requirePhase(t, phases, "layout")
			requirePhase(t, phases, "publication")
			requirePhase(t, phases, "finalization")

			requireCommandPhases(t, command, phases)

			if session == nil || session.Interrupted() {
				t.Fatal("healthy selected session unavailable")
			}
		})
	}
}

func TestProgressWriterFailureChangesOnlyEphemeralResponse(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	sink := &phaseRecordSink{failure: io.ErrClosedPipe}
	result := executeWith(t.Context(), t, appOf(newFake(t)), app.Env{
		WorkingDir: dir, NewProgress: func(ctx context.Context, mode cli.ProgressMode, attempt string) *app.Progress {
			return app.NewProgress(ctx, mode, attempt, sink)
		},
	}, commandCheck, sourceA, blankFlag, progressJSONFlag, reportFlag, progressReceipt)
	result.requireCode(t, 0, "")

	var stdout map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &stdout); err != nil {
		t.Fatal(err)
	}

	interrupted, valid := stdout["progress_interrupted"].(bool)
	if !valid || !interrupted {
		t.Fatal("optional sink failure absent from final response")
	}

	saved := readFile(t, dir+"/"+progressReceipt)
	if strings.Contains(saved, progressInterruptedField) {
		t.Fatal("ephemeral channel failure changed saved report")
	}
}

func phaseNamesFromRecords(t *testing.T, records [][]byte) map[string]bool {
	t.Helper()

	phases := map[string]bool{}

	for _, data := range records {
		var record struct {
			Event string `json:"event"`
			Phase string `json:"phase"`
		}
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}

		if record.Event == "phase" {
			phases[record.Phase] = true
		}
	}

	return phases
}

func requirePhase(t *testing.T, phases map[string]bool, phase string) {
	t.Helper()

	if !phases[phase] {
		t.Fatalf("real phase missing: %s", phase)
	}
}

func requireCommandPhases(t *testing.T, command string, phases map[string]bool) {
	t.Helper()

	backendPhases := []string{observation.Assembly.String(), "optimization", "output_writing", "output_verification"}
	for _, phase := range backendPhases {
		if command == commandCheck {
			if phases[phase] {
				t.Fatalf("check invented %s", phase)
			}
		} else {
			requirePhase(t, phases, phase)
		}
	}
}

func TestProgressCountsFailedInspectionAndRetainsFinalization(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	sink := &phaseRecordSink{}
	engine := newFake(t)
	engine.inspect = func(context.Context, string) (pdfengine.SourceInfo, error) {
		return pdfengine.SourceInfo{}, errBadSource
	}

	result := executeWith(t.Context(), t, appOf(engine), app.Env{
		WorkingDir: dir, NewProgress: func(ctx context.Context, mode cli.ProgressMode, attempt string) *app.Progress {
			return app.NewProgress(ctx, mode, attempt, sink)
		},
	}, commandCheck, sourceA, progressJSONFlag)
	if result.code == 0 {
		t.Fatal("failed source returned success")
	}

	requirePhase(t, phaseNamesFromRecords(t, sink.records), "finalization")

	completed := map[string]int64{}

	for _, data := range sink.records {
		var record struct {
			Completed *int64 `json:"completed"`
			Unit      string `json:"unit"`
		}
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}

		if record.Completed != nil {
			completed[record.Unit] = *record.Completed
		}
	}

	if completed["captured_sources"] != 1 || completed["processed_sources"] != 1 {
		t.Fatalf("failed inspection attempt counters %v", completed)
	}
}

func TestProgressFactoryRunsOnlyForParsedEligibleCommands(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{commandCheck, sourceA, "--progress=none"},
		{commandCheck, "--progress=invalid"},
		{commandVersion},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			dir := workDir(t)
			writePDF(t, dir, sourceA)

			called := false

			executeWith(t.Context(), t, appOf(newFake(t)), app.Env{
				WorkingDir: dir, NewProgress: func(context.Context, cli.ProgressMode, string) *app.Progress {
					called = true
					return nil
				},
			}, args...)

			if called {
				t.Fatal("unselected progress factory called")
			}
		})
	}
}
