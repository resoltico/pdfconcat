// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

type brokenCleanupPipe struct {
	err    error
	file   *os.File
	record []byte
}

func (sink *brokenCleanupPipe) WriteRecord(_ context.Context, record []byte) error {
	sink.record = bytes.Clone(record)
	_, sink.err = sink.file.Write(record)

	return sink.err
}

func TestNativeCleanupWarningPipeFailureAfterActualPublicationIsEphemeral(t *testing.T) {
	t.Parallel()

	for _, mode := range []cli.ProgressMode{cli.ProgressJSON, cli.ProgressAuto} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			runNativeCleanupWarningPipeFailure(t, mode)
		})
	}
}

func runNativeCleanupWarningPipeFailure(t *testing.T, mode cli.ProgressMode) {
	t.Helper()
	requireDirectoryPermissions(t)
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := writer.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}

	sink := &brokenCleanupPipe{file: writer}
	progress := &phaseRecordSink{}
	engine := newFake(t)
	engine.assemble = func(ctx context.Context, request *pdfengine.AssembleRequest) error {
		if cleanupErr := denyProgressWorkspaceCleanup(t, filepath.Dir(request.Destination)); cleanupErr != nil {
			return cleanupErr
		}

		return engine.realAssemble(ctx, request)
	}
	env := app.Env{
		WorkingDir: dir, ProgressRecord: sink,
		NewProgress: func(ctx context.Context, mode cli.ProgressMode, attempt string) *app.Progress {
			return app.NewProgress(ctx, mode, attempt, progress)
		},
	}
	result := executeWith(t.Context(), t, appOf(engine), env,
		commandBuild, sourceA, blankFlag, "--progress="+string(mode), reportFlag, progressReceipt, "-o", outputFile)

	parsed := result.requireCode(t, 0, "")
	if !parsed.Publication.Published || sink.err == nil {
		t.Fatal("native cleanup warning failure was not exercised after actual publication")
	}

	assertNativeCleanupWarningReceipt(t, result, sink, dir, mode)
}

func assertNativeCleanupWarningReceipt(t *testing.T, result outcome, sink *brokenCleanupPipe, dir string, mode cli.ProgressMode) {
	t.Helper()

	if mode == cli.ProgressAuto {
		if strings.Contains(result.stdout, progressInterruptedField) ||
			strings.Contains(readFile(t, dir+"/"+progressReceipt), progressInterruptedField) {
			t.Fatal("auto telemetry failure added a JSON progress-mode receipt")
		}

		if !bytes.HasPrefix(sink.record, []byte("pdfconcat: warning: ")) || !bytes.HasSuffix(sink.record, []byte("\n")) {
			t.Fatal("actual auto cleanup failure did not reach the idle text exception channel")
		}

		return
	}

	assertProgressInterruptedOnlyInResponse(t, result, dir)

	var warning struct {
		Kind     string `json:"kind"`
		Sequence uint64 `json:"sequence"`
	}
	if err := json.Unmarshal(sink.record, &warning); err != nil {
		t.Fatal(err)
	}

	if warning.Kind != "cleanup_warning" || warning.Sequence == 0 {
		t.Fatal("actual cleanup failure did not reach the idle structured exception channel")
	}
}

func denyProgressWorkspaceCleanup(t *testing.T, workspace string) error {
	t.Helper()

	stuck := filepath.Join(workspace, "unremovable")
	if err := os.Mkdir(stuck, 0o700); err != nil {
		return fmt.Errorf("create cleanup permission control: %w", err)
	}

	if err := os.WriteFile(filepath.Join(stuck, "file"), []byte("owned cleanup control"), 0o600); err != nil {
		return fmt.Errorf("populate cleanup permission control: %w", err)
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(workspace); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
	})
	// The permission helper registers exact restoration after this removal hook,
	// so its cleanup runs first and the owned workspace can then be removed.
	makeReadOnly(t, stuck)

	return nil
}
