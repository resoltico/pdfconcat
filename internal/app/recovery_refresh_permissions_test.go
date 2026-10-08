// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestRefreshPermissionFailurePreservesPlannedCompleteRecoveryAndPDF(t *testing.T) {
	t.Parallel()

	requireDirectoryPermissions(t)

	dir := workDir(t)
	writePDF(t, dir, sourceA)

	reports := filepath.Join(dir, "reports")
	if err := os.Mkdir(reports, 0o700); err != nil {
		t.Fatal(err)
	}

	output, target := filepath.Join(dir, outputFile), filepath.Join(reports, reportFile)
	runner := appOf(newFake(t))
	runner.AfterPDFCommit(func() {
		if err := os.Link(output, target); err != nil {
			t.Fatal(err)
		}

		makeReadOnly(t, reports)
	})
	res := execute(t.Context(), t, runner, dir, commandBuild, outputFlag, outputFile, reportFlag, target, overwriteFlag, sourceA)
	parsed := res.requireCode(t, 1, reportPublishFailureCode)

	var summary report.Summary
	decodeRecoverySummary(t, res.stdout, &summary)

	if summary.Publication.RecoveryState != report.RecoveryPending {
		t.Fatalf("planned recovery not marked: %+v", summary.Publication)
	}

	saved := readCompleteIdentityReport(t, parsed.Publication.RecoveryReport)
	if saved.Status != report.StatusOK || saved.Publication.ReportStatus != report.ReportWritten {
		t.Fatal("failed refresh destroyed original complete planned layout")
	}

	assertArtifactTypes(t, output, parsed.Publication.RecoveryReport)

	files, err := filepath.Glob(filepath.Join(reports, recoveryFilePattern))
	if err != nil || len(files) != 1 {
		t.Fatalf("refresh scratch leaked: %v %v", files, err)
	}
}
