// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type commitFixture struct {
	reportStage *Staged
	dir         string
	pdfStaged   string
	pdfTarget   string
	reportPath  string
}

const (
	pdfContent    = "pdf bytes"
	reportContent = `{"complete":true}`
)

func newCommitFixture(t *testing.T, ops operations) commitFixture {
	t.Helper()

	dir := t.TempDir()
	fixture := commitFixture{
		dir:        dir,
		pdfStaged:  filepath.Join(dir, "work.pdf"),
		pdfTarget:  filepath.Join(dir, outputPath),
		reportPath: filepath.Join(dir, "out.report.json"),
	}
	put(t, fixture.pdfStaged, pdfContent)

	fixture.reportStage = stageText(t, ops, fixture.reportPath, reportContent)

	return fixture
}

// stagedFiles counts the staging files left in dir.
func stagedFiles(t *testing.T, dir string) int {
	t.Helper()

	count := 0

	for _, name := range names(t, dir) {
		if strings.Contains(name, ".pdfconcat-report-") {
			count++
		}
	}

	return count
}

func discardStaged(t *testing.T, staged *Staged) {
	t.Helper()

	err := staged.Discard()
	if err != nil {
		t.Fatal(err)
	}
}

func (f commitFixture) pdf() PDF {
	return PDF{Staged: f.pdfStaged, Destination: f.pdfTarget}
}

func TestCommitPublishesPDFThenReport(t *testing.T) {
	t.Parallel()

	fixture := newCommitFixture(t, realOperations())

	result, err := Commit(context.Background(), fixture.pdf(), fixture.reportStage, Policy{})
	if err != nil || result != (Result{PDFPublished: true, ReportPublished: true}) {
		t.Fatalf(commitFailureFormat, result, err)
	}

	if get(t, fixture.pdfTarget) != pdfContent || get(t, fixture.reportPath) != reportContent {
		t.Fatal("published content is wrong")
	}

	if got := names(t, fixture.dir); len(got) != 2 {
		t.Fatalf("directory %v", got)
	}
}

func TestCommitWithoutReport(t *testing.T) {
	t.Parallel()

	fixture := newCommitFixture(t, realOperations())
	discardStaged(t, fixture.reportStage)

	result, err := Commit(context.Background(), fixture.pdf(), nil, Policy{})
	if err != nil || result != (Result{PDFPublished: true}) {
		t.Fatalf(commitFailureFormat, result, err)
	}
}

func TestCommitFailuresBeforeThePDFCommitPublishNothingAndDiscardTheReport(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		change   func(t *testing.T, f *commitFixture) PDF
		want     error
		canceled bool
	}{
		canceledAction: {func(_ *testing.T, f *commitFixture) PDF { return f.pdf() }, context.Canceled, true},
		"verification failed": {func(_ *testing.T, f *commitFixture) PDF {
			pdf := f.pdf()
			pdf.Verify = func() error { return errInjected }

			return pdf
		}, errInjected, false},
		"report target appeared": {func(t *testing.T, f *commitFixture) PDF {
			t.Helper()
			put(t, f.reportPath, "someone else's")

			return f.pdf()
		}, nil, false},
		"PDF target exists without overwrite": {func(t *testing.T, f *commitFixture) PDF {
			t.Helper()
			put(t, f.pdfTarget, existingContent)

			return f.pdf()
		}, nil, false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if test.canceled {
				cancel()
			}

			fixture := newCommitFixture(t, realOperations())
			pdf := test.change(t, &fixture)

			result, err := Commit(ctx, pdf, fixture.reportStage, Policy{})
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) || result != (Result{}) {
				t.Fatalf(commitFailureFormat, result, err)
			}

			if stagedFiles(t, fixture.dir) != 0 {
				t.Fatalf("staged report left behind: %v", names(t, fixture.dir))
			}

			if getIfPresent(t, fixture.pdfTarget) == pdfContent {
				t.Fatal("PDF was published")
			}
		})
	}
}

func TestLateReportFailureKeepsOneRecoveryFileAndNeverRollsBackThePDF(t *testing.T) {
	t.Parallel()

	var reportTarget string

	ops := realOperations()
	ops.replace = func(staged, target string, existing existingFile) error {
		if target == reportTarget {
			return errInjected
		}

		return replaceFile(staged, target, existing)
	}

	fixture := newCommitFixture(t, ops)
	reportTarget = fixture.reportPath

	result, err := commitWith(context.Background(), ops, fixture.pdf(), fixture.reportStage, Policy{})

	checkLateResult(t, result, err)
	checkRecoveryFile(t, fixture, result, err)
}

func checkLateResult(t *testing.T, result Result, err error) {
	t.Helper()
	t.Cleanup(func() {
		if closeErr := result.CloseRecovery(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	late, isLate := errors.AsType[*ReportPublishError](err)
	if !isLate || !errors.Is(err, errInjected) {
		t.Fatalf("commit() error = %v", err)
	}

	recoveredHere := result.RecoveryPath == late.RecoveryPath && late.Instruction == result.RecoveryInstruction
	if !result.PDFPublished || result.ReportPublished || !filepath.IsAbs(result.RecoveryPath) || !recoveredHere {
		t.Fatalf("result %+v", result)
	}
}

func checkRecoveryFile(t *testing.T, fixture commitFixture, result Result, err error) {
	t.Helper()

	if get(t, fixture.pdfTarget) != pdfContent || get(t, result.RecoveryPath) != reportContent {
		t.Fatal("PDF or recovery content is wrong")
	}

	if getIfPresent(t, fixture.reportPath) != "" {
		t.Fatal("report target exists")
	}

	message := err.Error()

	recoveries := stagedFiles(t, fixture.dir)
	if recoveries != 1 || !strings.Contains(message, "do not rebuild") || !strings.Contains(message, "was published") {
		t.Fatalf("recovery files %d; message %q", recoveries, err)
	}

	discardStaged(t, fixture.reportStage)

	if get(t, result.RecoveryPath) == "" {
		t.Fatal("Discard() removed the recovery file")
	}
}

func TestReportStillPublishesAfterCancellationFollowingThePDFCommit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	ops := realOperations()
	ops.replace = func(staged, target string, existing existingFile) error {
		err := replaceFile(staged, target, existing)

		cancel()

		return err
	}

	fixture := newCommitFixture(t, ops)

	result, err := commitWith(ctx, ops, fixture.pdf(), fixture.reportStage, Policy{})
	if err != nil || result != (Result{PDFPublished: true, ReportPublished: true}) {
		t.Fatalf("commit() = %+v, %v", result, err)
	}
}

func TestDirectoryFlushFailuresAfterCommitKeepPublishedState(t *testing.T) {
	t.Parallel()

	ops := realOperations()
	ops.syncDirectory = func(string) error { return errInjected }

	fixture := newCommitFixture(t, ops)

	result, err := commitWith(context.Background(), ops, fixture.pdf(), fixture.reportStage, Policy{})

	var durability *DurabilityError
	if !errors.As(err, &durability) || result != (Result{PDFPublished: true, ReportPublished: true}) {
		t.Fatalf("commit() = %+v, %v", result, err)
	}

	if get(t, fixture.pdfTarget) != pdfContent || get(t, fixture.reportPath) != reportContent {
		t.Fatal("published content is wrong")
	}
}

func TestRecoveryInstructionIsInTheCommandLanguageOfThePlatform(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin", "freebsd"} {
		posix := recoveryInstruction(goos, "/d/it's.partial", "/d/r.json")
		if !strings.Contains(posix, `cp -n -- '/d/it'\''s.partial' '/d/r.json'`) || strings.Contains(posix, "copy") {
			t.Errorf("%s instruction %q", goos, posix)
		}
	}

	windows := recoveryInstruction("windows", `C:\d\r.partial`, `C:\d\r.json`)
	if !strings.Contains(windows, `[System.IO.File]::Copy('C:\d\r.partial', 'C:\d\r.json', $false)`) || strings.Contains(windows, "cp -n") {
		t.Errorf("windows instruction %q", windows)
	}
}

func TestCommitWithoutAReportPublishesNothingWhenTheContextHasEnded(t *testing.T) {
	t.Parallel()

	fixture := newCommitFixture(t, realOperations())
	discardStaged(t, fixture.reportStage)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := Commit(ctx, fixture.pdf(), nil, Policy{})
	if !errors.Is(err, context.Canceled) || result != (Result{}) {
		t.Fatalf(commitFailureFormat, result, err)
	}

	if _, statErr := os.Lstat(fixture.pdfTarget); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("the PDF was published after cancellation: %v", statErr)
	}

	// A PDF that cannot be committed leaves nothing to discard either: the destination is a directory.
	err = os.Mkdir(fixture.pdfTarget, 0o700)
	if err != nil {
		t.Fatal(err)
	}

	result, err = Commit(t.Context(), fixture.pdf(), nil, Policy{})
	if err == nil || result.PDFPublished {
		t.Errorf("Commit() onto a directory = %+v, %v", result, err)
	}
}
