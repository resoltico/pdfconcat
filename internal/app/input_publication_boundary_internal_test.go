// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestNamedInputCancellationAfterInitialCheckReturnsNoOpenFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var opened *os.File

	file, err := openInputWith(ctx, path, func(name string) (*os.File, error) {
		var openErr error

		opened, openErr = capture.OpenRegular(name)

		cancel()

		return opened, openErr
	})
	if file != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("post-open cancellation: %v %v", file, err)
	}

	if opened == nil {
		t.Fatal("the regular file did not actually open")
	}

	if _, readErr := opened.Read(make([]byte, 1)); !errors.Is(readErr, os.ErrClosed) {
		t.Fatalf("canceled open leaked its owned handle: %v", readErr)
	}
}

func TestCanceledReportQueryClassifiesReadInterruption(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var stdout bytes.Buffer

	code := runQuery(
		ctx,
		&cli.Command{Name: cli.NameReport, ReportFile: "saved.json", Format: cli.FormatJSON},
		Env{WorkingDir: t.TempDir(), Stdout: &stdout},
	)
	if code != 130 {
		t.Fatalf("canceled query exit %d: %s", code, stdout.String())
	}

	if !bytes.Contains(stdout.Bytes(), []byte(`"status":"interrupted"`)) {
		t.Fatalf("canceled query status: %s", stdout.String())
	}
}

func TestReportVerificationRechecksLateOutputSourceAlias(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "output.pdf")

	if err := os.WriteFile(source, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := capture.NewRegistry()
	if _, err := registry.Add(capture.RoleSource, source); err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Add(capture.RoleOutput, output); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(source, output); err != nil {
		t.Fatal(err)
	}

	pipeline := &pipeline{registry: registry, output: output, reportPath: filepath.Join(dir, "report.json")}
	err := pipeline.verifyReport(pipeline.reportPath)

	alias, ok := errors.AsType[*capture.AliasError](err)
	if !ok || alias.OtherRole != capture.RoleSource {
		t.Fatalf("late alias accepted: %v", err)
	}
}

func TestReadFailureKeepsUnexpectedReadFailuresStructured(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	code := readFailure(
		Env{Stdout: &stdout},
		&cli.Command{Name: cli.NameReport, Format: cli.FormatJSON},
		"saved.json",
		"cannot read",
		errInjected,
	)
	if code != report.StatusFailed.ExitCode() || !bytes.Contains(stdout.Bytes(), []byte(`"code":"report_read_failed"`)) {
		t.Fatalf("read failure: exit %d %s", code, stdout.String())
	}
}
