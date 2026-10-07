// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestLateAliasRecoveryRecordsFailedPublicationAndQueriesWithoutUnsafeNext(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	output, target := filepath.Join(dir, outputFile), filepath.Join(dir, reportFile)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runner := appOf(newFake(t))
	runner.AfterPDFCommit(func() {
		if err := os.Link(output, target); err != nil {
			t.Fatal(err)
		}

		cancel()
	})
	res := execute(ctx, t, runner, dir, commandBuild, outputFlag, outputFile, reportFlag, reportFile, "--overwrite", sourceA)

	parsed := res.requireCode(t, 1, reportPublishFailureCode)
	if !parsed.Publication.Published || parsed.Publication.ReportStatus != failedState {
		t.Fatalf("committed state: %+v", parsed.Publication)
	}

	path := parsed.Publication.RecoveryReport
	if path == "" {
		t.Fatal("no complete recovery file")
	}

	assertCurrentRecovery(t, path, res.stdout)

	assertArtifactTypes(t, output, path)

	if readFile(t, output) != readFile(t, target) {
		t.Fatal("late alias no longer holds original PDF bytes")
	}

	query := execute(t.Context(), t, runner, dir, commandReport, path)
	if query.code != 0 {
		t.Fatalf("cannot query recovery: %s", query.stdout)
	}

	var summary report.Summary
	decodeRecoverySummary(t, query.stdout, &summary)

	if len(summary.Next) != 0 {
		t.Fatalf("unsafe next points original report alias: %v", summary.Next)
	}

	files, err := filepath.Glob(filepath.Join(dir, recoveryFilePattern))
	if err != nil || len(files) != 1 || files[0] != path {
		t.Fatalf("not exactly one useful recovery: %v %v", files, err)
	}
}

func decodeRecoverySummary(t *testing.T, data string, target *report.Summary) {
	t.Helper()

	if err := json.Unmarshal([]byte(data), target); err != nil {
		t.Fatal(err)
	}
}

func assertCurrentRecovery(t *testing.T, path, outcome string) {
	t.Helper()
	saved := readCompleteIdentityReport(t, path)

	current := saved.Status == report.StatusFailed && saved.Publication.ReportStatus == report.ReportFailed &&
		saved.Publication.RecoveryState == report.RecoveryCurrent && saved.Publication.RecoveryReport == path
	if !current {
		t.Fatalf("recovery publication is not current: %+v; command outcome: %s", saved.Publication, outcome)
	}
}
