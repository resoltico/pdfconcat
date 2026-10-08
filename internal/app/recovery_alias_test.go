// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestRecoveryOutputAliasPreservesStagedReportAndWithholdsCopyGuidance(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	output, target := filepath.Join(dir, outputFile), filepath.Join(dir, reportFile)

	var recovery, original string

	runner := appOf(newFake(t))
	runner.AfterPDFCommit(func() {
		recovery, original = aliasOutputWithStagedRecovery(t, dir, output, target)
	})

	res := execute(t.Context(), t, runner, dir, commandBuild, outputFlag, outputFile, reportFlag, reportFile, overwriteFlag, sourceA)
	res.requireCode(t, 1, reportPublishFailureCode)

	var summary report.Summary
	decodeRecoverySummary(t, res.stdout, &summary)

	if summary.Publication.RecoveryState != report.RecoveryUnavailable {
		t.Fatalf("output/report alias asserted as current recovery: %+v", summary.Publication)
	}

	if recovery == "" || readFile(t, recovery) != original {
		t.Fatal("alias rejection changed the original staged report")
	}

	assertUnavailableRecoveryGuidance(t, &summary)

	files, err := filepath.Glob(filepath.Join(dir, recoveryFilePattern))
	if err != nil || len(files) != 1 || files[0] != recovery {
		t.Fatalf("alias recovery scratch: %v %v", files, err)
	}
}

func aliasOutputWithStagedRecovery(t *testing.T, dir, output, target string) (string, string) {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, recoveryFilePattern))
	if err != nil || len(files) != 1 {
		t.Fatalf("staged report: %v %v", files, err)
	}

	recovery := files[0]
	original := readFile(t, recovery)
	// A concurrent writer replaces the published path with an alias of the still-owned report.
	if err = os.Remove(output); err != nil {
		t.Fatal(err)
	}

	if err = os.Link(recovery, output); err != nil {
		t.Fatal(err)
	}
	// The report cannot publish here, so its owned staged file becomes the recovery candidate.
	if err = os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}

	return recovery, original
}

func assertUnavailableRecoveryGuidance(t *testing.T, summary *report.Summary) {
	t.Helper()

	found := false

	for index := range summary.Diagnostics {
		diagnostic := &summary.Diagnostics[index]
		if diagnostic.Recovery != nil || strings.Contains(diagnostic.Message, "cp -n") ||
			strings.Contains(diagnostic.Message, "File.Copy") {
			t.Fatal("aliased recovery exposes copy guidance")
		}

		if diagnostic.Code != "report_recovery_refresh_failed" {
			continue
		}

		found = true

		if !strings.Contains(diagnostic.Message, "Recovery identity is unavailable") ||
			!strings.Contains(diagnostic.Message, "must not be copied") || strings.Contains(diagnostic.Message, "layout remains complete") {
			t.Fatalf("unavailable recovery presented as complete planned layout: %s", diagnostic.Message)
		}
	}

	if !found {
		t.Fatal("recovery alias has no refresh-failure diagnostic")
	}
}
