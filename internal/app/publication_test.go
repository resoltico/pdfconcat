// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

// reportNotRequested is the publication state of a command that was not asked for a report.
const reportNotRequested = "not_requested"

func TestPublicationNamesTheReportOnlyWhenOneWasRequested(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))

	with := execute(t.Context(), t, runner, dir, commandBuild, outputFlag, outputFile, reportFlag, reportFile, sourceA).
		requireCode(t, 0, "")

	wantWith := publicationView{
		Output: filepath.Join(dir, outputFile), ReportStatus: reportWritten, ReportPath: filepath.Join(dir, reportFile), Published: true,
	}
	if with.Publication != wantWith {
		t.Errorf("with a report: %+v", with.Publication)
	}

	without := execute(t.Context(), t, runner, dir, commandBuild, outputFlag, "plain.pdf", sourceA).requireCode(t, 0, "")

	wantWithout := publicationView{Output: filepath.Join(dir, "plain.pdf"), ReportStatus: reportNotRequested, Published: true}
	if without.Publication != wantWithout {
		t.Errorf("without a report: %+v", without.Publication)
	}
}

func TestAFailedBuildStillNamesItsDestinationAndSavesNothingUnrequested(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	fake := newFake(t)
	fake.assemble = func(context.Context, *pdfengine.AssembleRequest) error {
		return &pdfengine.Error{Code: pdfengine.CodeAssemblyFailed, Source: pdfengine.NoSource, Err: errPoolBroke}
	}

	res := execute(t.Context(), t, appOf(fake), dir, commandBuild, outputFlag, outputFile, sourceA)
	parsed := res.requireCode(t, 1, "assemble_failed")

	// Nobody asked for a report, so none is written and the failure is the only diagnostic.
	wantPublication := publicationView{Output: filepath.Join(dir, outputFile), ReportStatus: reportNotRequested}
	if parsed.Publication != wantPublication || parsed.DiagnosticCount != 1 {
		t.Errorf(publicationDiagnosticsFormat, parsed.Publication, parsed.DiagnosticCount)
	}

	requireMissing(t, filepath.Join(dir, outputFile))
}

func TestAReportThatCouldNotBePublishedIsNotWrittenAgainAsAFailureReport(t *testing.T) {
	t.Parallel()

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	runner := appOf(newFake(t))
	runner.BeforeCommit(func() { writeConcurrently(t, filepath.Join(dir, reportFile)) })

	res := execute(t.Context(), t, runner, dir, commandCheck, reportFlag, reportFile, sourceA)
	parsed := res.requireCode(t, 1, reportWriteFailureCode)

	// The failure is recorded once, and the report that someone else created is left alone.
	if parsed.DiagnosticCount != 1 || parsed.Publication.ReportStatus != failedState {
		t.Errorf(publicationDiagnosticsFormat, parsed.Publication, parsed.DiagnosticCount)
	}

	if readFile(t, filepath.Join(dir, reportFile)) != someoneElse {
		t.Error("the report of someone else was replaced")
	}
}

func TestTheSourceTheEngineBlamesIsFoundByItsPosition(t *testing.T) {
	t.Parallel()

	const sources = 2

	cases := map[string]struct {
		path   string
		source int
	}{
		"the first source":            {sourceA, 0},
		"the last source":             {sourceB, sources - 1},
		"a position after the last":   {"", sources},
		"a position before the first": {"", pdfengine.NoSource},
	}

	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := workDir(t)
			writePDF(t, dir, sourceA)
			writePDF(t, dir, sourceB)

			fake := newFake(t)
			fake.assemble = func(context.Context, *pdfengine.AssembleRequest) error {
				return &pdfengine.Error{Code: pdfengine.CodeLegacyDestsRepeated, Source: test.source, Err: errOverwrite}
			}

			res := execute(t.Context(), t, appOf(fake), dir, commandBuild, outputFlag, outputFile, sourceA, sourceB)
			parsed := res.requireCode(t, 2, "pdf_legacy_dests_repeated")

			want := ""
			if test.path != "" {
				want = filepath.Join(dir, test.path)
			}

			if parsed.Diagnostics[0].Path != want {
				t.Errorf("the diagnostic names %q, want %q", parsed.Diagnostics[0].Path, want)
			}
		})
	}
}
