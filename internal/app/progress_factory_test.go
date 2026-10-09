// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

const (
	progressRefusedFactory   = "refused"
	progressInterruptedField = "progress_interrupted"
)

func TestUnavailableProgressFactoryPreservesActualCheckAndBuild(t *testing.T) {
	t.Parallel()

	for _, command := range []string{commandCheck, commandBuild} {
		for _, factoryName := range []string{"absent", progressRefusedFactory} {
			t.Run(command+"/"+factoryName, func(t *testing.T) {
				t.Parallel()
				assertUnavailableProgressFactory(t, command, factoryName)
			})
		}
	}
}

func assertUnavailableProgressFactory(t *testing.T, command, factoryName string) {
	t.Helper()
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}

	called := 0

	env := app.Env{WorkingDir: dir}
	if factoryName == progressRefusedFactory {
		env.NewProgress = func(context.Context, cli.ProgressMode, string) *app.Progress { called++; return nil }
	}

	args := []string{command, sourceA, blankFlag, progressJSONFlag, reportFlag, progressReceipt}
	if command == commandBuild {
		args = append(args, "-o", outputFile)
	}

	result := executeWith(t.Context(), t, appOf(engine), env, args...)
	result.requireCode(t, 0, "")

	if factoryName == progressRefusedFactory && called != 1 {
		t.Fatal("parsed command did not attempt the selected factory exactly once")
	}

	assertProgressInterruptedOnlyInResponse(t, result, dir)
}

func TestNativeHealthRemainsAuthoritativeAfterHealthyProgressSession(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	engine, err := pdfengine.New()
	if err != nil {
		t.Fatal(err)
	}

	sink := &phaseRecordSink{}

	var session *app.Progress

	healthCalls := 0
	env := app.Env{
		WorkingDir: dir,
		NewProgress: func(ctx context.Context, mode cli.ProgressMode, attempt string) *app.Progress {
			session = app.NewProgress(ctx, mode, attempt, sink)
			return session
		},
		ProgressInterrupted: func() bool { healthCalls++; return true },
	}
	result := executeWith(t.Context(), t, appOf(engine), env,
		commandCheck, sourceA, blankFlag, progressJSONFlag, reportFlag, progressReceipt)
	result.requireCode(t, 0, "")

	if session == nil || session.Interrupted() || healthCalls == 0 {
		t.Fatal("healthy joined presenter lost the process owner's interruption observation")
	}

	assertProgressInterruptedOnlyInResponse(t, result, dir)
}

func assertProgressInterruptedOnlyInResponse(t *testing.T, result outcome, dir string) {
	t.Helper()

	var response struct {
		ProgressInterrupted bool `json:"progress_interrupted"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &response); err != nil {
		t.Fatal(err)
	}

	if !response.ProgressInterrupted || result.stderr != "" {
		t.Fatalf("optional unavailable sink outcome not captured: stdout=%s stderr=%s", result.stdout, result.stderr)
	}

	if strings.Contains(readFile(t, dir+"/"+progressReceipt), progressInterruptedField) {
		t.Fatal("optional sink health changed the persisted report")
	}
}
