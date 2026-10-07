// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestRecoveryIdentityLossPreservesUnknownReplacementAndForbidsCopyGuidance(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	output, target := filepath.Join(dir, outputFile), filepath.Join(dir, reportFile)

	var replaced string

	const foreign = "unknown writer's bytes must remain"

	runner := appOf(newFake(t))
	runner.AfterPDFCommit(func() {
		files, err := filepath.Glob(filepath.Join(dir, recoveryFilePattern))
		if err != nil || len(files) != 1 {
			t.Fatalf("staged report: %v %v", files, err)
		}

		replaced = files[0]
		if removeErr := os.Remove(replaced); removeErr != nil {
			t.Fatal(removeErr)
		}

		writeFile(t, replaced, foreign)

		if linkErr := os.Link(output, target); linkErr != nil {
			t.Fatal(linkErr)
		}
	})
	res := execute(t.Context(), t, runner, dir, commandBuild, outputFlag, outputFile, reportFlag, reportFile, overwriteFlag, sourceA)
	res.requireCode(t, 1, reportPublishFailureCode)

	var summary report.Summary
	decodeRecoverySummary(t, res.stdout, &summary)

	if summary.Publication.RecoveryState != report.RecoveryUnavailable {
		t.Fatalf("unknown recovery falsely asserted complete: %+v", summary.Publication)
	}

	if readFile(t, replaced) != foreign {
		t.Fatal("unknown recovery inode overwritten")
	}

	for _, diagnostic := range summary.Diagnostics {
		if strings.Contains(diagnostic.Message, "cp -n") || strings.Contains(diagnostic.Message, "File.Copy") {
			t.Fatal("unavailable recovery exposes executable copy guidance")
		}
	}

	if !strings.HasPrefix(readFile(t, output), "%PDF-") || readFile(t, output) != readFile(t, target) {
		t.Fatal("published PDF was changed")
	}
}
