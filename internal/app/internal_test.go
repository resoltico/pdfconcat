// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/genpage"
	"github.com/resoltico/pdfconcat/internal/pdfengine"
	"github.com/resoltico/pdfconcat/internal/plan"
	"github.com/resoltico/pdfconcat/internal/publish"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type (
	// failingWriter is a terminal that fails every write and counts the attempts.
	failingWriter struct{ calls int }

	// closeFails is a file that writes and then cannot be closed.
	closeFails struct{ io.Writer }

	// resourceSinkFailure writes real bytes before a sink fault, then closes the real handle.
	resourceSinkFailure struct{ file *os.File }

	resourceMergeCounter struct {
		Engine

		calls int
	}

	// planThatCannotBeClosed is a plan whose bytes read fine and whose file then fails to close.
	planThatCannotBeClosed struct{ io.Reader }

	// classification is a failure and the diagnostic and status classify must make of it.
	classification struct {
		err    error
		name   string
		code   report.Code
		status report.Status
		path   string
	}

	// planStatus is a plan error code and the status it implies.
	planStatus struct {
		code   plan.Code
		status report.Status
	}
)

// Words and timestamps that several tests repeat.
const (
	shortMessage = "short"
	commitTime   = "2026-10-05T09:34:20Z"
	checkName    = "check"
	buildName    = "build"
)

// Failures the tests inject, and the volume-went-away error of a file that cannot be closed.
var (
	errInjected    = errors.New("injected failure")
	errPlain       = errors.New("plain failure")
	errCloseFailed = errors.New("the volume went away")
)

func TestClassifyMapsEveryKindOfFailure(t *testing.T) {
	t.Parallel()

	missingFile := &fs.PathError{Op: "open", Path: "/u", Err: fs.ErrNotExist}
	cases := []classification{
		{fmt.Errorf("copy: %w", context.Canceled), "interruption", codeInterrupted, report.StatusInterrupted, ""},
		{context.DeadlineExceeded, "deadline", codeInterrupted, report.StatusInterrupted, ""},
		{&capture.AliasError{Path: "/a"}, "alias", codeAliasConflict, report.StatusInvalid, "/a"},
		{
			&capture.ArtifactTargetError{Path: "/o", Problem: "is a directory"},
			"artifact target", codeArtifactTarget, report.StatusInvalid, "/o",
		},
		{
			&capture.NotRegularFileError{Path: "/p", Kind: "a named pipe"},
			"not a regular file", codeSourceNotRegular, report.StatusInvalid, "/p",
		},
		{&capture.SourceChangedError{Path: "/c"}, "changed while copied", codeSourceChanged, report.StatusFailed, "/c"},
		{
			&capture.ScratchError{Dir: "/s", Operation: "copy", Err: errInjected},
			"scratch", codeScratchFailed, report.StatusFailed, "",
		},
		{
			&capture.SourceError{Path: "/u", Operation: "open source", Err: missingFile},
			"unreadable", codeSourceUnreadable, report.StatusFailed, "/u",
		},
		{
			&pdfengine.Error{Code: pdfengine.CodeInvalid, Err: errInjected},
			"engine invalid", "pdf_invalid", report.StatusFailed, "",
		},
		{
			&pdfengine.Error{Code: pdfengine.CodePartialRange, Err: errInjected},
			"engine partial range", "pdf_partial_range_unsupported", report.StatusInvalid, "",
		},
		{errInjected, "other", codeOperationFailed, report.StatusFailed, ""},
	}

	for _, test := range cases {
		got := classify(stageInput, test.err)

		matches := got.diagnostic.Code == test.code && got.status == test.status && got.diagnostic.Path == test.path

		if !matches || got.diagnostic.Stage != stageInput {
			t.Errorf(namedFailureFormat, test.name, got)
		}
	}

	got := classify(stageAssemble, fmt.Errorf("write: %w", errDiskFull))
	if got.diagnostic.Code != codeDiskFull || got.status != report.StatusFailed {
		t.Errorf("a full volume under any failure is named: %+v", got)
	}

	got = classify(stageInput, fmt.Errorf("%w", context.Canceled))
	if got.diagnostic.Code != codeInterrupted {
		t.Errorf("cancellation wins: %+v", got)
	}
}

func TestReasonAndBoundedMessages(t *testing.T) {
	t.Parallel()

	if got := reasonOf(&fs.PathError{Op: "stat", Path: "/very/long/path", Err: fs.ErrNotExist}); got != "file does not exist" {
		t.Errorf("a path error loses its path: %q", got)
	}

	if got := reasonOf(errPlain); got != "plain failure" {
		t.Errorf("reason: %q", got)
	}

	long := strings.Repeat("ā", maxMessageRunes+50)

	cut := boundedMessage(long)
	if !strings.HasSuffix(cut, "…") || len([]rune(cut)) != maxMessageRunes+1 {
		t.Errorf("cut message has %d characters", len([]rune(cut)))
	}

	if boundedMessage(shortMessage) != shortMessage {
		t.Error("a short message changed")
	}
}

func TestCombineAndSharedMessages(t *testing.T) {
	t.Parallel()

	invalid := problem{status: report.StatusInvalid}
	failed := problem{status: report.StatusFailed}
	cut := problem{status: report.StatusInterrupted}

	if combine([]problem{invalid, failed}) != report.StatusInvalid || combine([]problem{failed, invalid}) != report.StatusFailed {
		t.Error("the first problem decides")
	}

	if combine([]problem{failed, cut, invalid}) != report.StatusInterrupted {
		t.Error("an interruption wins")
	}

	single := &assembly.Error{Message: "one", Affected: 1}
	if sharedMessage(single) != "one" {
		t.Errorf("single: %q", sharedMessage(single))
	}

	shared := &assembly.Error{Message: "overflow", Affected: 3, Related: []assembly.Location{{Pointer: "/items/4"}, {Pointer: "/items/9"}}}
	if got := sharedMessage(shared); got != "overflow (3 entries use this; also at /items/4, /items/9)" {
		t.Errorf("shared: %q", got)
	}

	counted := &assembly.Error{Message: "m", Affected: 2}
	if got := sharedMessage(counted); got != "m (2 entries use this)" {
		t.Errorf("counted: %q", got)
	}
}

func TestPlanAndAssemblyProblemsOutsideTheirContracts(t *testing.T) {
	t.Parallel()

	if got := planProblem(errInjected); got.diagnostic.Code != codeOperationFailed || got.status != report.StatusFailed {
		t.Errorf("a decoding failure that is not a plan error: %+v", got)
	}

	for _, test := range []planStatus{
		{plan.CodeInterrupted, report.StatusInterrupted},
		{plan.CodeReadFailed, report.StatusFailed},
		{plan.CodeSyntax, report.StatusInvalid},
	} {
		got := planProblem(&plan.Error{Code: test.code, Stage: plan.StageSyntax, Message: "m"})
		if got.status != test.status {
			t.Errorf("%s: %v, want %v", test.code, got.status, test.status)
		}
	}

	current := &pipeline{builder: report.NewBuilder(checkName), status: report.StatusOK}

	err := current.stopAssembly(stageLayout, fmt.Errorf("placing: %w", context.Canceled))
	if !errors.Is(err, errStopped) || current.status != report.StatusInterrupted {
		t.Errorf("an interruption inside a resolution step: %v %v", err, current.status)
	}
}

func TestProgressDrawsStagesAndBoundsRedraws(t *testing.T) {
	t.Parallel()

	var (
		out   bytes.Buffer
		clock = time.Unix(0, 0)
	)

	progress := newProgress(&out, func() time.Time { return clock })

	progress.enter(stageInspect)

	// Steps closer together than the redraw interval draw nothing.
	progress.step(1, 10)

	clock = clock.Add(minRedrawInterval / 2)

	progress.step(2, 10)

	if strings.Contains(out.String(), "1/10") || strings.Contains(out.String(), "2/10") {
		t.Errorf("a redraw inside the interval: %q", out.String())
	}

	clock = clock.Add(minRedrawInterval)

	progress.step(3, 10)

	if !strings.Contains(out.String(), "pdfconcat: inspect 3/10") {
		t.Errorf("the counter was not drawn: %q", out.String())
	}

	// A flood of steps over a long time stops at the redraw bound.
	for done := range 5 * maxRedraws {
		clock = clock.Add(2 * minRedrawInterval)

		progress.step(done, 5*maxRedraws)
	}

	if progress.redraws != maxRedraws {
		t.Errorf("%d redraws, want exactly %d", progress.redraws, maxRedraws)
	}

	progress.erase()
	progress.erase()

	if !strings.HasSuffix(out.String(), "\r") {
		t.Errorf("the line was not cleared: %q", out.String()[max(0, out.Len()-30):])
	}

	if strings.Contains(out.String(), "\x1b") {
		t.Error("progress uses terminal escape sequences")
	}
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++

	return 0, errInjected
}

func TestProgressNeverFailsTheCommand(t *testing.T) {
	t.Parallel()

	silent := newProgress(nil, time.Now)
	silent.enter(stagePrepare)
	silent.step(1, 2)
	silent.erase()

	broken := &failingWriter{}
	progress := newProgress(broken, time.Now)
	progress.enter(stagePrepare)
	progress.enter(stageInspect)
	progress.step(1, 2)
	progress.erase()

	if broken.calls != 1 {
		t.Errorf("%d writes to a broken terminal; the first failure should disable progress", broken.calls)
	}
}

func TestResolveBuildFillsWhatTheLinkerDidNot(t *testing.T) {
	t.Parallel()

	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.time", Value: commitTime},
			{Key: "vcs.modified", Value: "true"},
			{Key: "unrelated", Value: "x"},
		},
	}

	cases := []struct {
		info        *debug.BuildInfo
		name        string
		version     string
		wantVersion string
		wantCommit  string
		wantDate    string
	}{
		{info, "linker version wins", "1.0.0", "1.0.0", "0123456789ab+dirty", commitTime},
		{info, "module version cannot override project configuration", "", configuredVersion(), "0123456789ab+dirty", commitTime},
		{
			&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			"a development build",
			"",
			configuredVersion(),
			unknownMetadata,
			unknownMetadata,
		},
		{
			&debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261005093420-0f945b7b88c9"}},
			"module pseudo-version cannot override project configuration",
			"",
			configuredVersion(),
			unknownMetadata,
			unknownMetadata,
		},
		{nil, "no build information", "", configuredVersion(), unknownMetadata, unknownMetadata},
		{&debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: shortMessage}, {Key: "vcs.modified", Value: "false"},
		}}, "a clean checkout has no dirty mark", "", configuredVersion(), shortMessage, unknownMetadata},
	}

	for _, test := range cases {
		got := ResolveBuild(test.version, test.info)
		if got.Version != test.wantVersion || got.Commit != test.wantCommit || got.CommitDate != test.wantDate {
			t.Errorf(namedFailureFormat, test.name, got)
		}
	}
}

func TestJobCountIsBoundedBySourcesAndDefaults(t *testing.T) {
	t.Parallel()

	current := &pipeline{command: &cli.Command{}}

	if got := current.jobCount(1000); got != min(4, runtime.GOMAXPROCS(0)) {
		t.Errorf("default jobs %d", got)
	}

	if got := current.jobCount(2); got > 2 || got < 1 {
		t.Errorf("never more workers than sources: %d", got)
	}

	current.command.Jobs = 7

	if current.jobCount(100) != 7 || current.jobCount(3) != 3 || current.jobCount(0) != 1 {
		t.Errorf("explicit jobs: %d %d %d", current.jobCount(100), current.jobCount(3), current.jobCount(0))
	}
}

func TestStagingAnInvalidReportFails(t *testing.T) {
	t.Parallel()

	current := &pipeline{reportPath: t.TempDir() + "/r.json"}

	_, err := current.stageFile(t.Context(), current.reportPath, &report.Report{})
	if err == nil {
		t.Fatal("an invalid report was staged")
	}

	reported := current.reportProblem(err)
	if reported.diagnostic.Code != report.CodeInvalidValue && reported.diagnostic.Code != report.CodeUnsupportedVersion {
		t.Errorf("the report package's own diagnostic is kept: %+v", reported.diagnostic)
	}

	// An oversized report is the report package's failure too, and keeps its stable code.
	valid := report.NewBuilder(checkName)
	valid.SetPhases(report.Phases{
		Instructions: report.PhaseComplete, InputInspection: report.PhaseComplete, Layout: report.PhaseComplete,
		OutputVerification: report.PhaseNotRun,
	})

	zero := int64(0)
	valid.SetCounts(report.Counts{SourcePages: &zero, GeneratedPages: &zero, TotalPages: &zero})

	_, err = report.Write(io.Discard, valid.Build(report.StatusOK), 3)

	if got := current.reportProblem(err).diagnostic.Code; got != report.CodeTooLarge {
		t.Errorf(unexpectedCodeFormat, got)
	}

	other := current.reportProblem(&publish.StageError{Target: "/t", Err: errInjected})
	if other.diagnostic.Code != codeReportWrite || other.diagnostic.Path != current.reportPath {
		t.Errorf("a staging failure: %+v", other.diagnostic)
	}
}

// openScratch is a private workspace that the test removes when it ends.
func openScratch(tb testing.TB) *capture.Workspace {
	tb.Helper()

	workspace, err := capture.NewTemporaryWorkspace()
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		closeErr := workspace.Close()
		if closeErr != nil {
			tb.Error(closeErr)
		}
	})

	return workspace
}

func TestResourceProducerCannotWriteOutsideItsWorkspace(t *testing.T) {
	t.Parallel()

	workspace := openScratch(t)
	if err := os.RemoveAll(workspace.Dir()); err != nil {
		t.Fatal(err)
	}

	current := &pipeline{
		workspace: workspace,
		progress:  newProgress(nil, time.Now),
		builder:   report.NewBuilder(buildName),
		layout:    &assembly.Layout{Specs: []assembly.ResolvedSpec{{}}},
	}
	if err := current.placeResources(t.Context()); !errors.Is(err, errStopped) || current.resource != nil {
		t.Fatalf("resource creation failure: %v", err)
	}
}

func TestCanceledResourceProductionKeepsLayoutIncomplete(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	current := &pipeline{
		workspace: openScratch(t),
		progress:  newProgress(nil, time.Now),
		builder:   report.NewBuilder(buildName),
		layout: &assembly.Layout{
			Specs: []assembly.ResolvedSpec{{Spec: assembly.BlankSpec{Dim: assembly.PageDim{Width: 100, Height: 100}}}},
		},
	}
	if err := current.placeResources(ctx); !errors.Is(err, errStopped) {
		t.Fatalf("canceled resource production: %v", err)
	}

	result := current.builder.Build(current.status)
	if current.resource != nil || current.phases.Layout == report.PhaseComplete || current.status != report.StatusInterrupted ||
		len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code != codeInterrupted {
		t.Fatalf("canceled production truth: %+v diagnostics=%+v", current.phases, result.Diagnostics)
	}
}

func TestAnUnflushedDirectoryIsAWarningNotAFailure(t *testing.T) {
	t.Parallel()

	current := &pipeline{builder: report.NewBuilder(buildName), output: "/out.pdf", status: report.StatusOK}

	err := current.afterCommit(
		t.Context(),
		&publish.Result{PDFPublished: true},
		&publish.DurabilityError{Path: "/out.pdf", Err: errInjected},
	)
	if err != nil || len(current.warnings) != 1 || !current.publication.Published || current.status != report.StatusOK {
		t.Fatalf("%v %v %+v", err, current.warnings, current.publication)
	}

	if !strings.Contains(current.warnings[0], "may not survive a crash") {
		t.Errorf("warning %q", current.warnings[0])
	}
}

func TestFailuresOutsideTheirContractsStillReport(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	env := Env{Stdout: &stdout}

	code := usageFailure(env, errInjected)
	if code != 2 || !strings.Contains(stdout.String(), "command_unsupported") {
		t.Errorf("%d %s", code, stdout.String())
	}

	stdout.Reset()

	code = queryFailure(env, &cli.Command{Name: cli.NameReport}, errInjected)
	if code != 1 || !strings.Contains(stdout.String(), "operation_failed") {
		t.Errorf("%d %s", code, stdout.String())
	}

	stdout.Reset()

	usage := &cli.UsageError{
		Diagnostics: []report.Diagnostic{{Stage: report.StageUsage, Code: "usage_x", Message: "m"}},
		Format:      cli.FormatText,
	}
	code = usageFailure(env, usage)

	if code != 2 || !strings.Contains(stdout.String(), "usage_x") || strings.HasPrefix(stdout.String(), "{") {
		t.Errorf("a text-format usage failure: %d %q", code, stdout.String())
	}
}

func (closeFails) Close() error { return errCloseFailed }

func (f resourceSinkFailure) Write(data []byte) (int, error) {
	n, err := f.file.Write(data[:min(len(data), 16)])
	return n, errors.Join(err, errInjected)
}

func (f *resourceMergeCounter) Assemble(context.Context, *pdfengine.AssembleRequest) error {
	f.calls++
	return nil
}

func (f resourceSinkFailure) Close() error { return errors.Join(f.file.Close(), errCloseFailed) }

func TestResourceSinkFailureCannotCompleteLayoutOrReachMerge(t *testing.T) {
	t.Parallel()
	workspace := openScratch(t)
	path := workspace.NewPath(generatedPDFExtension)

	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, scratchMode)
	if err != nil {
		t.Fatal(err)
	}

	font, err := typeset.LoadDefaultFont()
	if err != nil {
		t.Fatal(err)
	}

	style := assembly.BlankStyle{}

	spec, err := style.Resolve(assembly.PageDim{Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}

	engine := &resourceMergeCounter{}

	current := &pipeline{
		workspace: workspace,
		pdf:       engine,
		fonts:     loadedFonts{byPath: map[string]*typeset.Font{"": font}},
		builder:   report.NewBuilder(buildName),
		layout: &assembly.Layout{
			Specs: []assembly.ResolvedSpec{{Spec: spec}},
		},
	}
	if failure := current.produceResource(t.Context(), path, resourceSinkFailure{file: file}); !errors.Is(failure, errStopped) {
		t.Fatalf("sink failure: %v", failure)
	}

	assertFailedResource(t, current, engine)

	if _, readErr := file.Read(make([]byte, 1)); !errors.Is(readErr, fs.ErrClosed) {
		t.Fatalf("resource handle left open: %v", readErr)
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-1.7")) {
		t.Fatalf("native file bytes not exercised: %q %v", data, err)
	}

	if removalErr := os.Remove(path); removalErr != nil {
		t.Fatal(removalErr)
	}
}

func assertFailedResource(t *testing.T, current *pipeline, engine *resourceMergeCounter) {
	t.Helper()

	result := current.builder.Build(current.status)
	if engine.calls != 0 || current.resource != nil || current.phases.Layout == report.PhaseComplete ||
		current.status != report.StatusFailed ||
		len(result.Diagnostics) != 1 ||
		result.Diagnostics[0].Code != codeRenderFailed {
		t.Fatalf("failed resource outcome: phases=%+v diagnostics=%+v", current.phases, result.Diagnostics)
	}

	message := result.Diagnostics[0].Message
	if !strings.Contains(message, errInjected.Error()) || !strings.Contains(message, errCloseFailed.Error()) {
		t.Fatalf("lost independent write/close failures: %s", message)
	}
}

func TestResourceProductionKeepsWriteCloseAndCancellationFailures(t *testing.T) {
	t.Parallel()

	produce := func(int) (genpage.Page, error) { return genpage.Page{Width: 10, Height: 10}, nil }

	err := writeResource(t.Context(), closeFails{Writer: io.Discard}, 1, nil, produce)
	if !errors.Is(err, errCloseFailed) || !strings.Contains(err.Error(), "finish the generated-pages file") {
		t.Fatalf("close failure: %v", err)
	}

	err = writeResource(t.Context(), closeFails{Writer: &failingWriter{}}, 1, nil, produce)
	if !errors.Is(err, errCloseFailed) || !errors.Is(err, errInjected) {
		t.Fatalf("write and close failures: %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	err = writeResource(canceled, closeFails{Writer: io.Discard}, 1, nil, produce)
	if !errors.Is(err, errCloseFailed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation and close failure: %v", err)
	}
}

func TestFontPlumbingThatTheJobNeverExercises(t *testing.T) {
	t.Parallel()

	if _, found := (&loadedFonts{}).digest("/missing.ttf"); found {
		t.Error("a font that was never loaded has a digest")
	}

	// The private copy of a font vanishes between capture and reading.
	workspace := openScratch(t)

	source := filepath.Join(t.TempDir(), "font.ttf")

	err := os.WriteFile(source, []byte("bytes"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	set := capture.NewSet(workspace)

	captured, err := set.Capture(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}

	if err = os.Remove(captured.Path); err != nil {
		t.Fatal(err)
	}

	current := &pipeline{registry: capture.NewRegistry(), captures: set, workspace: workspace}

	_, err = current.loadFont(t.Context(), &assembly.FontFileUse{Path: source})

	scratch, isScratch := errors.AsType[*capture.ScratchError](err)
	if !isScratch || scratch.Dir != workspace.Dir() {
		t.Errorf("reading a vanished copy: %v", err)
	}
}

func TestInstructionsThatNoDecoderProducesAreStillRejected(t *testing.T) {
	t.Parallel()

	// A job with no source cannot be flattened; the pipeline reports it instead of failing later.
	current := &pipeline{builder: report.NewBuilder(checkName), status: report.StatusOK, job: &assembly.Job{}}

	if err := current.flatten(); !errors.Is(err, errStopped) || current.status != report.StatusFailed {
		t.Errorf("flattening a job without a source: %v %v", err, current.status)
	}

	// A plan output cannot resolve against a base directory that is not absolute.
	current = &pipeline{
		builder: report.NewBuilder(checkName), status: report.StatusOK, command: &cli.Command{Name: cli.NameCheck},
		job: &assembly.Job{
			Source: assembly.ArgumentSource{}, Base: "relative", Output: assembly.Set(outputPath, assembly.Origin{}),
		},
	}

	err := current.resolveOutput()
	if !errors.Is(err, errStopped) || current.status != report.StatusInvalid {
		t.Fatalf("resolving the output: %v %v", err, current.status)
	}

	if got := current.builder.Build(report.StatusInvalid).Diagnostics[0].Code; got != codePathInvalid {
		t.Errorf(unexpectedCodeFormat, got)
	}
}

func TestEncodingFailureStillProducesOutput(t *testing.T) {
	t.Parallel()

	if got := string(encodeLine(make(chan int))); got != `{"kind":"encoding_failed"}`+"\n" {
		t.Errorf("fallback %q", got)
	}

	if got := string(encodeLine(map[string]int{"a": 1})); got != `{"a":1}`+"\n" {
		t.Errorf("line %q", got)
	}
}

func TestAPlanSourceThePipelineDoesNotKnowIsAMissingPlan(t *testing.T) {
	t.Parallel()

	current := &pipeline{
		builder: report.NewBuilder(checkName), status: report.StatusOK,
		command: &cli.Command{Name: cli.NameCheck, PlanSource: cli.PlanSource(200)},
	}

	err := current.loadJob(t.Context())
	if !errors.Is(err, errStopped) || current.status != report.StatusInvalid {
		t.Fatalf("loading a job from an unknown source: %v %v", err, current.status)
	}

	found := current.builder.Build(report.StatusInvalid).Diagnostics[0]
	if found.Code != "plan_missing" || !strings.Contains(found.Message, "plan source 200 is not supported") {
		t.Errorf("diagnostic %+v", found)
	}
}

func (planThatCannotBeClosed) Close() error { return errCloseFailed }

func TestAPlanFileThatCannotBeClosedIsUnreadable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	current := &pipeline{
		builder: report.NewBuilder(checkName), status: report.StatusOK, command: &cli.Command{Name: cli.NameCheck},
	}
	file := planThatCannotBeClosed{strings.NewReader(`{"version":1,"items":["a.pdf"]}`)}

	err := current.decodePlanFile(t.Context(), filepath.Join(dir, planPath), file)
	if !errors.Is(err, errStopped) || current.status != report.StatusFailed || current.job != nil {
		t.Fatalf("closing the plan: %v %v %v", err, current.status, current.job)
	}

	found := current.builder.Build(report.StatusFailed).Diagnostics[0]
	if found.Code != codePlanUnreadable || !strings.Contains(found.Message, errCloseFailed.Error()) {
		t.Errorf("diagnostic %+v", found)
	}

	// The same plan that closes cleanly is accepted, so the failure above is the close and not the plan.
	current = &pipeline{
		builder: report.NewBuilder(checkName), status: report.StatusOK, command: &cli.Command{Name: cli.NameCheck},
	}

	clean := io.NopCloser(strings.NewReader(`{"version":1,"items":["a.pdf"]}`))

	err = current.decodePlanFile(t.Context(), filepath.Join(dir, planPath), clean)
	if err != nil || current.job == nil {
		t.Errorf("a plan that closes cleanly: %v %v", err, current.job)
	}
}
