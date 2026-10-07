// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

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
	"sync/atomic"
	"testing"

	"github.com/resoltico/pdfconcat/internal/app"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/pdffixture"
	"github.com/resoltico/pdfconcat/internal/report"
)

type (
	// fakeEngine delegates to the real engine except where a test replaces an operation, which is how
	// failures are injected at a chosen stage.
	fakeEngine struct {
		engine   *pdfengine.Engine
		inspect  func(ctx context.Context, path string) (pdfengine.SourceInfo, error)
		assemble func(ctx context.Context, request *pdfengine.AssembleRequest) error
	}

	// outcome is what one command printed.
	outcome struct {
		stdout, stderr string
		code           int
	}

	// publicationView is the publication state of a summary.
	publicationView struct {
		Output         string `json:"output"`
		ReportStatus   string `json:"report_status"`
		ReportPath     string `json:"report_path"`
		RecoveryReport string `json:"recovery_report"`
		Published      bool   `json:"published"`
	}

	// diagnosticView is a diagnostic of a summary, with only the members the tests read.
	diagnosticView struct {
		Stage   string `json:"stage"`
		Code    string `json:"code"`
		Path    string `json:"path"`
		Message string `json:"message"`
	}

	// summaryView is the part of a summary the tests read.
	summaryView struct {
		Phases          map[string]string `json:"phases"`
		Status          string            `json:"status"`
		Command         string            `json:"command"`
		Diagnostics     []diagnosticView  `json:"diagnostics"`
		Publication     publicationView   `json:"publication"`
		DiagnosticCount int               `json:"diagnostic_count"`
	}

	// assemblyFailure is an injected engine failure at assembly and what the command must make of it.
	assemblyFailure struct {
		err    error
		code   string
		status int
	}

	// failingReader is a standard input that breaks on the first read.
	failingReader struct{}

	// brokenWriter is a standard stream whose reader has gone away.
	brokenWriter struct{}
)

// Words of the command line and of the summaries that several tests use.
const (
	commandBuild       = "build"
	commandCheck       = "check"
	commandReport      = "report"
	commandVersion     = "version"
	commandHelp        = "help"
	flagFormat         = "--format"
	formatText         = "text"
	reportWritten      = "written"
	reportFile         = "r.json"
	phaseIncomplete    = "incomplete"
	outcomeInterrupted = "interrupted"
)

// Failures the tests inject. They are sentinels because the commands under test report what they are given.
var (
	errPoolBroke    = errors.New("pool broke")
	errPageCount    = errors.New("the output has 3 pages, expected 4")
	errXrefDamaged  = errors.New("xref damaged")
	errOverwrite    = errors.New("overwrite")
	errNoEngine     = errors.New("no engine today")
	errBadSource    = errors.New("bad")
	errPipeBroke    = errors.New("the pipe broke")
	errStreamClosed = errors.New("the stream is closed")
)

func (failingReader) Read([]byte) (int, error) { return 0, errPipeBroke }

func (brokenWriter) Write([]byte) (int, error) { return 0, errStreamClosed }

func (f *fakeEngine) Inspect(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
	if f.inspect != nil {
		return f.inspect(ctx, path)
	}

	return f.realInspect(ctx, path)
}

func (f *fakeEngine) Assemble(ctx context.Context, request *pdfengine.AssembleRequest) error {
	if f.assemble != nil {
		return f.assemble(ctx, request)
	}

	return f.realAssemble(ctx, request)
}

// realInspect is the real engine's Inspect, for a replacement that only adds a side effect.
func (f *fakeEngine) realInspect(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
	info, err := f.engine.Inspect(ctx, path)
	if err != nil {
		return info, fmt.Errorf("real engine: %w", err)
	}

	return info, nil
}

// realAssemble is the real engine's Assemble, for a replacement that only adds a side effect.
func (f *fakeEngine) realAssemble(ctx context.Context, request *pdfengine.AssembleRequest) error {
	err := f.engine.Assemble(ctx, request)
	if err != nil {
		return fmt.Errorf("real engine: %w", err)
	}

	return nil
}

func newFake(tb testing.TB) *fakeEngine {
	tb.Helper()

	engine, err := pdfengine.New()
	if err != nil {
		tb.Fatal(err)
	}

	return &fakeEngine{engine: engine}
}

func appOf(engine app.Engine) *app.App {
	return app.New(func() (app.Engine, error) { return engine, nil })
}

// workDir is a temporary directory without symbolic-link components.
func workDir(tb testing.TB) string {
	tb.Helper()

	dir, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}

	return dir
}

// writePDF writes a one-page PDF named name into dir.
func writePDF(tb testing.TB, dir, name string) {
	tb.Helper()

	err := pdffixture.Pages(strings.TrimSuffix(name, pdfExtension), 1).WriteFile(filepath.Join(dir, name))
	if err != nil {
		tb.Fatal(err)
	}
}

// execute runs args in dir.
func execute(ctx context.Context, tb testing.TB, runner *app.App, dir string, args ...string) outcome {
	tb.Helper()

	return executeWith(ctx, tb, runner, app.Env{WorkingDir: dir, Stdin: strings.NewReader("")}, args...)
}

func executeWith(ctx context.Context, tb testing.TB, runner *app.App, env app.Env, args ...string) outcome {
	tb.Helper()

	var stdout, stderr bytes.Buffer

	if env.Stdout == nil {
		env.Stdout = &stdout
	}

	if env.Stderr == nil {
		env.Stderr = &stderr
	}

	code := runner.Run(ctx, args, env)

	return outcome{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func (o outcome) summary(tb testing.TB) summaryView {
	tb.Helper()

	var parsed summaryView

	err := json.Unmarshal([]byte(o.stdout), &parsed)
	if err != nil {
		tb.Fatalf("stdout is not a JSON summary: %v\n%.400s", err, o.stdout)
	}

	return parsed
}

func (o outcome) requireCode(tb testing.TB, code int, diagnostic string) summaryView {
	tb.Helper()

	parsed := o.summary(tb)
	if o.code != code {
		tb.Fatalf("exit %d, want %d\nstdout: %.600s\nstderr: %s", o.code, code, o.stdout, o.stderr)
	}

	if diagnostic == "" {
		return parsed
	}

	for _, found := range parsed.Diagnostics {
		if found.Code == diagnostic {
			return parsed
		}
	}

	tb.Fatalf("no diagnostic %q in %+v", diagnostic, parsed.Diagnostics)

	return parsed
}

// requireClean fails if dir holds scratch left by a command.
func requireClean(tb testing.TB, dir string) {
	tb.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		tb.Fatal(err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pdfconcat-job-") || strings.Contains(entry.Name(), ".pdfconcat-report-") {
			tb.Fatalf("leftover %s", entry.Name())
		}
	}
}

func requireMissing(tb testing.TB, path string) {
	tb.Helper()

	_, err := os.Lstat(path)
	if !errors.Is(err, os.ErrNotExist) {
		tb.Fatalf("%s should not exist: %v", path, err)
	}
}

// readFile returns the content of path, which is a file the test itself arranged.
func readFile(tb testing.TB, path string) string {
	tb.Helper()

	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		tb.Fatal(err)
	}

	return string(content)
}

func TestEngineFailuresAtAssemblyAreReportedAndNothingIsPublished(t *testing.T) {
	t.Parallel()

	sourceError := func(code pdfengine.Code, source int, err error) error {
		return &pdfengine.Error{Code: code, Source: source, Err: err}
	}

	cases := map[string]assemblyFailure{
		"backend failure": {sourceError(pdfengine.CodeAssemblyFailed, pdfengine.NoSource, errPoolBroke), "assemble_failed", 1},
		"count mismatch": {
			sourceError(pdfengine.CodeOutputPageCount, pdfengine.NoSource, errPageCount), "output_page_count_mismatch", 1,
		},
		"invalid output": {sourceError(pdfengine.CodeOutputInvalid, pdfengine.NoSource, errXrefDamaged), "output_invalid", 1},
		"unsupported repeat": {
			&pdfengine.Error{Code: pdfengine.CodeLegacyDestsRepeated, Source: 1, Path: "/private/scratch", Err: errOverwrite},
			"pdf_legacy_dests_repeated", 2,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := workDir(t)
			writePDF(t, dir, sourceA)
			writePDF(t, dir, sourceB)

			fake := newFake(t)
			fake.assemble = func(context.Context, *pdfengine.AssembleRequest) error { return test.err }

			res := execute(t.Context(), t, appOf(fake), dir, commandBuild, "-o", outputFile, reportFlag, reportFile, sourceA, sourceB)
			parsed := res.requireCode(t, test.status, test.code)

			if parsed.Publication.Published || parsed.Phases["output_verification"] != phaseIncomplete {
				t.Errorf("publication or phase: %+v", parsed)
			}

			if parsed.Publication.ReportStatus != reportWritten {
				t.Errorf("the failure was not recorded in the requested report: %+v", parsed.Publication)
			}

			if test.status == 2 && !strings.HasSuffix(parsed.Diagnostics[0].Path, sourceB) {
				t.Errorf("the diagnostic names %q instead of the source the engine blamed", parsed.Diagnostics[0].Path)
			}

			if strings.Contains(res.stdout, "scratch") {
				t.Errorf("a private scratch path leaked into the output: %s", res.stdout)
			}

			requireMissing(t, filepath.Join(dir, outputFile))
			requireClean(t, dir)
		})
	}
}

func TestDiskFullIsNamed(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	fake := newFake(t)
	fake.assemble = func(context.Context, *pdfengine.AssembleRequest) error {
		return &pdfengine.Error{Code: pdfengine.CodeAssemblyFailed, Source: pdfengine.NoSource, Err: errNoSpace}
	}

	res := execute(t.Context(), t, appOf(fake), dir, commandBuild, "-o", outputFile, sourceA)
	res.requireCode(t, 1, "disk_full")
	requireClean(t, dir)
}

func TestEngineCreationFailure(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := app.New(func() (app.Engine, error) { return nil, errNoEngine })

	res := execute(t.Context(), t, runner, dir, commandCheck, sourceA)
	res.requireCode(t, 1, "engine_unavailable")
}

func TestInspectionFailuresKeepInputOrderAcrossWorkers(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(workDir(t), strings.Repeat("inspection-", 16))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	args := make([]string, 0, 33)
	args = append(args, commandCheck, "--jobs", "8")

	for index := range 30 {
		name := "f" + string(rune('a'+index%26)) + string(rune('a'+index/26)) + pdfExtension
		writePDF(t, dir, name)
		args = append(args, name)
	}

	fake := newFake(t)
	fake.inspect = func(_ context.Context, path string) (pdfengine.SourceInfo, error) {
		// Every source fails; the arrival order of failures depends on scheduling.
		return pdfengine.SourceInfo{}, &pdfengine.Error{
			Code:   pdfengine.CodeInvalid,
			Source: pdfengine.NoSource,
			Path:   path,
			Err:    errBadSource,
		}
	}

	res := execute(t.Context(), t, appOf(fake), dir, append(args, reportFlag, reportFile)...)
	parsed := res.requireCode(t, 1, codePDFInvalid)

	if parsed.DiagnosticCount != 30 {
		t.Fatalf("%d diagnostics, want 30", parsed.DiagnosticCount)
	}

	if len(res.stdout) > 2048 {
		t.Fatalf("summary is %d bytes, want at most 2048", len(res.stdout))
	}

	requireInspectionFailureOrder(t, filepath.Join(dir, reportFile), dir, args[3:])
}

func requireInspectionFailureOrder(t *testing.T, path, dir string, operands []string) {
	t.Helper()

	saved, err := report.Decode(t.Context(), path, strings.NewReader(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}

	if saved.Status != report.StatusFailed || len(saved.Diagnostics) != len(operands) {
		t.Fatalf("saved report status %s has %d diagnostics, want failed with all %d", saved.Status, len(saved.Diagnostics), len(operands))
	}

	for index, found := range saved.Diagnostics {
		want := filepath.Join(dir, operands[index])
		if found.Path != want {
			t.Errorf("diagnostic %d is about %s, want %s", index, found.Path, want)
		}

		if found.Location == nil || found.Location.ArgvIndex == nil || *found.Location.ArgvIndex != 3+index {
			t.Errorf("diagnostic %d has location %+v, want argv index %d", index, found.Location, 3+index)
		}
	}
}

func TestInspectionWorkersNeverExceedTheJobLimit(t *testing.T) {
	t.Parallel()

	dir := workDir(t)

	var (
		args    = make([]string, 0, 15)
		current atomic.Int64
		peak    atomic.Int64
	)

	args = append(args, commandCheck, "--jobs", "2")

	for index := range 12 {
		name := "s" + string(rune('a'+index)) + pdfExtension
		writePDF(t, dir, name)
		args = append(args, name)
	}

	fake := newFake(t)
	fake.inspect = func(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
		now := current.Add(1)
		defer current.Add(-1)

		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}

		return fake.realInspect(ctx, path)
	}

	res := execute(t.Context(), t, appOf(fake), dir, args...)
	res.requireCode(t, 0, "")

	if peak.Load() > 2 {
		t.Errorf("%d concurrent inspections with --jobs 2", peak.Load())
	}
}

// writeConcurrently creates path as another process would between the last check and publication.
func writeConcurrently(tb testing.TB, path string) {
	tb.Helper()

	err := os.WriteFile(path, []byte("someone else\n"), 0o600)
	if err != nil {
		tb.Error(err)
	}
}

// cancelAt returns a context and the function that cancels it.
func cancelAt(tb testing.TB) (context.Context, context.CancelFunc) {
	tb.Helper()

	return context.WithCancel(tb.Context())
}

func TestCancellationAtEveryStageStopsWithoutPublishing(t *testing.T) {
	t.Parallel()

	stages := map[string]func(fake *fakeEngine, runner *app.App, cancel context.CancelFunc){
		"inspect": func(fake *fakeEngine, _ *app.App, cancel context.CancelFunc) {
			fake.inspect = func(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
				cancel()

				return fake.realInspect(ctx, path)
			}
		},
		"assembly": func(fake *fakeEngine, _ *app.App, cancel context.CancelFunc) {
			fake.assemble = func(ctx context.Context, request *pdfengine.AssembleRequest) error {
				cancel()

				return fake.realAssemble(ctx, request)
			}
		},
		"before publication": func(_ *fakeEngine, runner *app.App, cancel context.CancelFunc) {
			runner.BeforeCommit(cancel)
		},
	}

	for name, arrange := range stages {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := workDir(t)
			writePDF(t, dir, sourceA)
			writePDF(t, dir, sourceB)

			fake := newFake(t)
			runner := appOf(fake)
			ctx, cancel := cancelAt(t)
			arrange(fake, runner, cancel)

			res := execute(ctx, t, runner, dir, commandBuild, "-o", outputFile, reportFlag, reportFile, sourceA, "--blank", sourceB)
			parsed := res.requireCode(t, 130, outcomeInterrupted)

			if parsed.Status != outcomeInterrupted || parsed.Publication.Published {
				t.Errorf(summaryFailureFormat, parsed)
			}

			// An interrupted run still describes itself, in a new report.
			if parsed.Publication.ReportStatus != reportWritten {
				t.Errorf("report: %+v", parsed.Publication)
			}

			requireMissing(t, filepath.Join(dir, outputFile))
			requireClean(t, dir)
		})
	}
}

func TestCancellationBeforeAnythingStarts(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	ctx, cancel := cancelAt(t)
	cancel()

	res := execute(ctx, t, appOf(newFake(t)), dir, commandCheck, sourceA)
	res.requireCode(t, 130, outcomeInterrupted)
}

func TestStandardInputReadIsCancellable(t *testing.T) {
	t.Parallel()

	dir := workDir(t)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := errors.Join(writer.Close(), reader.Close())
		if closeErr != nil {
			t.Error(closeErr)
		}
	})

	ctx, cancel := cancelAt(t)
	done := make(chan outcome, 1)

	go func() {
		done <- executeWith(ctx, t, appOf(newFake(t)), app.Env{WorkingDir: dir, Stdin: reader}, commandCheck, "--plan", "-")
	}()

	cancel()

	res := <-done
	res.requireCode(t, 130, outcomeInterrupted)
}

func TestStandardInputReadFailureIsAnOperationalFailure(t *testing.T) {
	t.Parallel()

	res := execute2(t, app.Env{WorkingDir: workDir(t), Stdin: failingReader{}}, commandCheck, "--plan", "-")
	res.requireCode(t, 1, "read_failed")
}

func execute2(tb testing.TB, env app.Env, args ...string) outcome {
	tb.Helper()

	return executeWith(tb.Context(), tb, appOf(newFake(tb)), env, args...)
}

func TestPostPublicationReportFailureKeepsThePDFAndARecoveryReport(t *testing.T) {
	t.Parallel()

	requireDirectoryPermissions(t)

	dir := workDir(t)
	reports := filepath.Join(dir, "reports")

	err := os.Mkdir(reports, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))

	var restore func()

	runner.BeforeCommit(func() { restore = makeReadOnly(t, reports) })

	res := execute(t.Context(), t, runner, dir, commandBuild, "-o", outputFile, reportFlag, "reports/r.json", sourceA)
	parsed := res.requireCode(t, 1, reportPublishFailureCode)

	if !parsed.Publication.Published || parsed.Publication.ReportStatus != "failed" || parsed.Publication.RecoveryReport == "" {
		t.Fatalf(publicationFailureFormat, parsed.Publication)
	}

	if !strings.Contains(parsed.Diagnostics[0].Message, "Do not rebuild") {
		t.Errorf("the recovery instruction is missing: %s", parsed.Diagnostics[0].Message)
	}

	_, err = os.Stat(filepath.Join(dir, outputFile))
	if err != nil {
		t.Errorf("the PDF was not kept: %v", err)
	}

	// The recovery report is the complete report; saving it where it was meant to go makes it true.
	restore()

	recovered := execute(t.Context(), t, runner, dir, commandReport, parsed.Publication.RecoveryReport)
	saved := recovered.requireCode(t, 0, "")

	if saved.Status != "ok" || saved.Publication.ReportPath != filepath.Join(reports, reportFile) {
		t.Errorf("the recovery report: %+v", saved)
	}
}

func TestConcurrentCreationOfTheDestinationIsNotOverwritten(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))
	runner.BeforeCommit(func() { writeConcurrently(t, filepath.Join(dir, outputFile)) })

	res := execute(t.Context(), t, runner, dir, commandBuild, "-o", outputFile, reportFlag, reportFile, sourceA)
	parsed := res.requireCode(t, 1, "publish_failed")

	if parsed.Publication.Published {
		t.Errorf(publicationFailureFormat, parsed.Publication)
	}

	if content := readFile(t, filepath.Join(dir, outputFile)); content != someoneElse {
		t.Errorf("the concurrently created file was replaced: %q", content)
	}

	requireClean(t, dir)
}

func TestConcurrentCreationOfTheReportStopsBeforeThePDFIsPublished(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))
	runner.BeforeCommit(func() { writeConcurrently(t, filepath.Join(dir, reportFile)) })

	res := execute(t.Context(), t, runner, dir, commandBuild, "-o", outputFile, reportFlag, reportFile, sourceA)
	parsed := res.requireCode(t, 1, "publish_failed")

	if parsed.Publication.Published {
		t.Errorf("the PDF was published although the report could not be: %+v", parsed.Publication)
	}

	requireMissing(t, filepath.Join(dir, outputFile))
	requireClean(t, dir)
}

func TestScratchCleanupFailureIsAWarningNotAFailure(t *testing.T) {
	t.Parallel()

	requireDirectoryPermissions(t)

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	var (
		stuck   string
		restore func()
	)

	fake := newFake(t)
	fake.assemble = func(ctx context.Context, request *pdfengine.AssembleRequest) error {
		// A directory inside the workspace that cannot be emptied.
		stuck = filepath.Join(filepath.Dir(request.Destination), "stuck")

		err := os.Mkdir(stuck, 0o700)
		if err != nil {
			return fmt.Errorf("create the stuck directory: %w", err)
		}

		err = os.WriteFile(filepath.Join(stuck, "file"), []byte("x"), 0o600)
		if err != nil {
			return fmt.Errorf("fill the stuck directory: %w", err)
		}

		restore = makeReadOnly(t, stuck)

		return fake.realAssemble(ctx, request)
	}

	t.Cleanup(func() {
		if stuck == "" {
			return
		}

		restore()

		err := os.RemoveAll(filepath.Dir(stuck))
		if err != nil {
			t.Error(err)
		}
	})

	res := execute(t.Context(), t, appOf(fake), dir, commandBuild, "-o", outputFile, sourceA)
	res.requireCode(t, 0, "")

	if !strings.Contains(res.stderr, "warning: could not remove the scratch directory "+filepath.Dir(stuck)) {
		t.Errorf("stderr: %q", res.stderr)
	}
}

func TestUnwritableStreamsAreReportedWithoutPanicking(t *testing.T) {
	t.Parallel()

	for _, format := range []string{"json", formatText} {
		dir := workDir(t)
		writePDF(t, dir, sourceA)

		var stderr bytes.Buffer

		env := app.Env{WorkingDir: dir, Stdout: brokenWriter{}, Stderr: &stderr}

		res := executeWith(t.Context(), t, appOf(newFake(t)), env, commandBuild, flagFormat, format, "-o", outputFile, sourceA)
		if res.code != 1 {
			t.Errorf("%s: exit %d, want 1 after a committed build whose result could not be printed", format, res.code)
		}

		if !strings.Contains(stderr.String(), `"published":true`) {
			t.Errorf("%s: standard error lacks the committed state: %q", format, stderr.String())
		}

		if _, err := os.Stat(filepath.Join(dir, outputFile)); err != nil {
			t.Errorf("%s: %v", format, err)
		}
	}

	// Nothing can be reported when both streams are broken; the command still ends with its status.
	env := app.Env{WorkingDir: workDir(t), Stdout: brokenWriter{}, Stderr: brokenWriter{}}
	for _, args := range [][]string{{commandVersion}, {commandHelp}, {"schema", "plan"}, {"bogus"}, {commandCheck, "nothing.pdf"}} {
		res := executeWith(t.Context(), t, appOf(newFake(t)), env, args...)
		if res.code == 0 {
			t.Errorf("%v: exit 0 although standard output failed", args)
		}
	}

	// A nil error stream is allowed and silent.
	env.Stderr = nil

	if code := appOf(newFake(t)).Run(t.Context(), []string{commandVersion}, env); code != 1 {
		t.Errorf("exit %d", code)
	}
}

func TestExecuteRejectsWhatItCannotRun(t *testing.T) {
	t.Parallel()

	runner := appOf(newFake(t))

	var stdout bytes.Buffer

	env := app.Env{WorkingDir: workDir(t), Stdout: &stdout}
	code := runner.Execute(t.Context(), &cli.Command{Name: "frobnicate"}, env)

	if code != 2 || !strings.Contains(stdout.String(), "command_unsupported") {
		t.Errorf(exitFailureFormat, code, stdout.String())
	}

	stdout.Reset()

	env.WorkingDir = "relative"
	code = runner.Execute(t.Context(), &cli.Command{Name: cli.NameCheck}, env)

	if code != 1 || !strings.Contains(stdout.String(), "working_directory_unavailable") {
		t.Errorf(exitFailureFormat, code, stdout.String())
	}

	stdout.Reset()

	code = runner.Execute(t.Context(), &cli.Command{Name: cli.NameSchema, SchemaName: "nonsense"}, env)
	if code != 2 || !strings.Contains(stdout.String(), "schema_unknown") {
		t.Errorf(exitFailureFormat, code, stdout.String())
	}

	stdout.Reset()

	code = runner.Execute(t.Context(), &cli.Command{Name: cli.NameCheck}, app.Env{WorkingDir: workDir(t), Stdout: &stdout})
	if code != 2 || !strings.Contains(stdout.String(), "plan_missing") {
		t.Errorf("a check without instructions: exit %d: %s", code, stdout.String())
	}
}

func TestAnOperandJobWithoutOperandsIsInvalid(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	env := app.Env{WorkingDir: workDir(t), Stdout: &stdout}
	code := appOf(newFake(t)).Execute(t.Context(), &cli.Command{Name: cli.NameCheck, PlanSource: cli.PlanOperands}, env)

	if code != 2 || !strings.Contains(stdout.String(), "job_invalid") {
		t.Errorf(exitFailureFormat, code, stdout.String())
	}
}

func TestQueryOutputFailureAndTextHelp(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))
	execute(t.Context(), t, runner, dir, commandCheck, reportFlag, reportFile, sourceA).requireCode(t, 0, "")

	var stderr bytes.Buffer

	env := app.Env{WorkingDir: dir, Stdout: brokenWriter{}, Stderr: &stderr}

	for _, args := range [][]string{
		{commandReport, reportFile},
		{commandReport, reportFile, flagFormat, formatText},
		{commandVersion, flagFormat, formatText},
		{commandHelp, flagFormat, formatText},
		{commandHelp},
		{"schema", commandReport},
	} {
		stderr.Reset()

		if code := runner.Run(t.Context(), args, env); code != 1 || !strings.Contains(stderr.String(), "cannot write standard output") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr.String())
		}
	}
}

func TestConcurrentCreationOfACheckReportIsNotOverwritten(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))
	runner.BeforeCommit(func() { writeConcurrently(t, filepath.Join(dir, reportFile)) })

	res := execute(t.Context(), t, runner, dir, commandCheck, reportFlag, reportFile, sourceA)
	parsed := res.requireCode(t, 1, reportWriteFailureCode)

	if parsed.Publication.ReportStatus != "failed" {
		t.Errorf(publicationFailureFormat, parsed.Publication)
	}

	if content := readFile(t, filepath.Join(dir, reportFile)); content != someoneElse {
		t.Errorf("the concurrently created report was replaced: %q", content)
	}

	requireClean(t, dir)
}
