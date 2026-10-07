// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

// sourceProblem is a command line whose sources have problems, and what the command must report about them.
type sourceProblem struct {
	instructions string
	inspection   string
	codes        []string
	args         []string
	exit         int
}

// Codes and words of the source checks.
const (
	codeUnreadable = "source_unreadable"
	phaseInspected = "input_inspection"
	jobsFlag       = "--jobs"
	notAPDF        = "this is not a PDF\n"
)

func requireSourceProblems(t *testing.T, test *sourceProblem) {
	t.Helper()

	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writePDF(t, dir, sourceB)

	before := readFile(t, filepath.Join(dir, sourceB))
	res := execute(t.Context(), t, appOf(newFake(t)), dir, test.args...)
	parsed := res.requireCode(t, test.exit, test.codes[0])

	if len(parsed.Diagnostics) != len(test.codes) {
		t.Fatalf(diagnosticsFailureFormat, parsed.Diagnostics)
	}

	for index, code := range test.codes {
		if parsed.Diagnostics[index].Code != code {
			t.Errorf("diagnostic %d is %s, want %s", index, parsed.Diagnostics[index].Code, code)
		}
	}

	if parsed.Phases["instructions"] != test.instructions || parsed.Phases[phaseInspected] != test.inspection {
		t.Errorf("phases %v", parsed.Phases)
	}

	if readFile(t, filepath.Join(dir, sourceB)) != before {
		t.Error("a source was changed")
	}

	requireClean(t, dir)
}

func TestEverySourceIsRegisteredAndProblemsAreOrderedAliasesFirst(t *testing.T) {
	t.Parallel()

	cases := map[string]sourceProblem{
		"an alias after a good source": {
			phaseIncomplete,
			stateNotRun,
			[]string{codeAlias},
			[]string{commandBuild, overwriteFlag, outputFlag, sourceB, sourceA, sourceB},
			2,
		},
		"a missing source": {
			"complete", phaseIncomplete, []string{codeUnreadable}, []string{commandCheck, sourceA, "missing.pdf"}, 1,
		},
		"an alias and a missing source": {
			phaseIncomplete, stateNotRun,
			[]string{codeAlias, codeUnreadable},
			[]string{commandBuild, overwriteFlag, outputFlag, sourceB, "missing.pdf", sourceB},
			2,
		},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			requireSourceProblems(t, &test)
		})
	}
}

func TestAFailureReportMayReplaceAFileOnlyOnceEveryInputIsKnown(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writeFile(t, filepath.Join(dir, "corrupt.pdf"), notAPDF)

	runner := appOf(newFake(t))

	// Every input is known when the second source cannot be inspected, so --overwrite governs the report.
	writeFile(t, filepath.Join(dir, reportFile), previousReportContent)

	res := execute(t.Context(), t, runner, dir, commandCheck, overwriteFlag, reportFlag, reportFile, sourceA, "corrupt.pdf")
	parsed := res.requireCode(t, 1, "")

	if parsed.Publication.ReportStatus != reportWritten || parsed.DiagnosticCount != 1 {
		t.Errorf(publicationDiagnosticsFormat, parsed.Publication, parsed.DiagnosticCount)
	}

	if saved := readFile(t, filepath.Join(dir, reportFile)); !strings.Contains(saved, `"kind":"report"`) {
		t.Errorf("the report was not replaced: %.80q", saved)
	}

	// An invalid plan is found before the inputs are known: the report may only be a new file.
	writeFile(t, filepath.Join(dir, reportFile), previousReportContent)

	res = execute(t.Context(), t, runner, dir, commandCheck, overwriteFlag, reportFlag, reportFile, inlinePlanFlag, "{", detailsFlag)
	parsed = res.requireCode(t, 2, "")

	if parsed.Publication.ReportStatus != failedState || len(parsed.Diagnostics) != 2 {
		t.Fatalf("publication %+v, diagnostics %+v", parsed.Publication, parsed.Diagnostics)
	}

	secondary := parsed.Diagnostics[1]

	explained := strings.Contains(secondary.Message, "does not apply") && strings.Contains(secondary.Message, "until every input is known")
	if secondary.Code != reportWriteFailureCode || !explained {
		t.Errorf("secondary diagnostic %+v", secondary)
	}

	if readFile(t, filepath.Join(dir, reportFile)) != previousReportContent {
		t.Error("an existing file was replaced before every input was known")
	}
}

func TestAFailureReportCannotReplaceTheFileItDescribes(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	content := planJSON(t, map[string]any{keyItems: []any{sourceA}})
	writeFile(t, filepath.Join(dir, planFile), content)

	res := execute(
		t.Context(),
		t,
		appOf(newFake(t)),
		dir,
		commandCheck,
		planFlag,
		planFile,
		reportFlag,
		planFile,
		overwriteFlag,
		detailsFlag,
	)
	parsed := res.requireCode(t, 2, codeAlias)

	if len(parsed.Diagnostics) != 2 {
		t.Fatalf(diagnosticsFailureFormat, parsed.Diagnostics)
	}

	// The report could not be saved because its path is the plan, and that is said with the code of the conflict.
	described := strings.HasPrefix(parsed.Diagnostics[1].Message, "the failure report could not be saved")
	if parsed.Diagnostics[1].Code != codeAlias || !described {
		t.Errorf("secondary diagnostic %+v", parsed.Diagnostics[1])
	}

	if readFile(t, filepath.Join(dir, planFile)) != content {
		t.Error("the plan was replaced by the report")
	}
}

func TestOnlyTheSourcesThatFailInspectionAreReportedInOrderOfFirstUse(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)
	writeFile(t, filepath.Join(dir, sourceB), notAPDF)

	res := execute(t.Context(), t, appOf(newFake(t)), dir, commandCheck, sourceA, sourceB)
	parsed := res.requireCode(t, 1, "")

	if parsed.DiagnosticCount != 1 || parsed.Diagnostics[0].Path != filepath.Join(dir, sourceB) {
		t.Errorf(diagnosticsFailureFormat, parsed.Diagnostics)
	}

	// A source used twice is reported where it is first used, so that the order does not follow the last use.
	for _, name := range []string{sourceCPath, sourceDPath} {
		writePDF(t, dir, name)
	}

	fake := newFake(t)
	fake.inspect = func(_ context.Context, path string) (pdfengine.SourceInfo, error) {
		return pdfengine.SourceInfo{}, &pdfengine.Error{
			Code:   pdfengine.CodeInvalid,
			Source: pdfengine.NoSource,
			Path:   path,
			Err:    errBadSource,
		}
	}

	res = execute(t.Context(), t, appOf(fake), dir, commandCheck, jobsFlag, "3", sourceCPath, sourceDPath, sourceCPath)
	parsed = res.requireCode(t, 1, codePDFInvalid)

	inOrder := filepath.Base(parsed.Diagnostics[0].Path) == sourceCPath && filepath.Base(parsed.Diagnostics[1].Path) == sourceDPath
	if parsed.DiagnosticCount != 2 || !inOrder {
		t.Errorf(diagnosticsFailureFormat, parsed.Diagnostics)
	}
}

func TestCancellationStopsHandingOutSources(t *testing.T) {
	t.Parallel()

	const sources = 300

	dir := workDir(t)

	args := append(make([]string, 0, sources+3), commandCheck, jobsFlag, "1")

	for index := range sources {
		name := fmt.Sprintf("s%03d.pdf", index)
		writePDF(t, dir, name)

		args = append(args, name)
	}

	var (
		inspected atomic.Int64
		fake      = newFake(t)
	)

	ctx, cancel := cancelAt(t)
	fake.inspect = func(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
		inspected.Add(1)
		cancel()

		return fake.realInspect(ctx, path)
	}

	res := execute(ctx, t, appOf(fake), dir, args...)
	res.requireCode(t, 130, outcomeInterrupted)

	// The source in progress and at most a few that were already handed out are inspected; the rest are not.
	if got := inspected.Load(); got > sources/10 {
		t.Errorf("%d of %d sources were inspected after the command was interrupted", got, sources)
	}
}
