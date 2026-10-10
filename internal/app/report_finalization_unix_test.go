// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package app_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckReportCommitSurvivesDirectoryFlushFailure(t *testing.T) {
	requireDirectoryPermissions(t)
	dir := workDir(t)
	runner := appOf(newFake(t))
	runner.BeforeCommit(func() {
		if err := os.Chmod(dir, 0300); err != nil {
			t.Fatal(err)
		}
	})
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0700); err != nil {
			t.Error(err)
		}
	})
	res := execute(t.Context(), t, runner, dir, "check", "--plan-json", `{"version":1,"items":[{"blank":{"size":"100x100"}}]}`, "--report", "proof.json")
	parsed := res.requireCode(t, 0, "")
	if parsed.Publication.ReportStatus != reportWritten {
		t.Fatalf("committed report was described as unwritten: %+v", parsed.Publication)
	}
	if _, err := os.Stat(filepath.Join(dir, "proof.json")); err != nil {
		t.Fatalf("native report commit absent: %v", err)
	}
}
