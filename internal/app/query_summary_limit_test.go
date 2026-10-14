// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestSavedReportSummaryLimitRejectsBeforeOutputAndPreservesEvidence(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	saved := readFile(t, "../report/testdata/summary_required_facts.json")
	path := filepath.Join(dir, reportFile)
	writeFile(t, path, saved)
	before := sha256.Sum256([]byte(readFile(t, path)))
	for _, format := range []string{"json", "text"} {
		out := execute(t.Context(), t, appOf(newFake(t)), dir, commandReport, reportFile, "--format", format)
		if out.code != 1 || len(out.stdout) > 2048 || out.stderr != "" {
			t.Fatalf("%s limit result code%d bytes%d stderr%q", format, out.code, len(out.stdout), out.stderr)
		}
		if format == "json" {
			var failure report.CommandError
			if err := json.Unmarshal([]byte(out.stdout), &failure); err != nil {
				t.Fatal(err)
			}
			if failure.Kind != "error" || failure.Status != report.StatusFailed || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != report.CodeResponseTooLarge || failure.Diagnostics[0].Recovery == nil || failure.Diagnostics[0].Recovery.ReportFrom != "original_argv.report_operand" {
				t.Fatalf("false success or lost recovery: %+v", failure)
			}
		} else if !strings.Contains(out.stdout, "response_too_large") || !strings.Contains(out.stdout, "--view diagnostics --details") {
			t.Fatalf("unactionable failure %q", out.stdout)
		}
	}
	detailed := execute(t.Context(), t, appOf(newFake(t)), dir, commandReport, reportFile, "--view", "diagnostics", "--details")
	if detailed.code != 0 || !strings.Contains(detailed.stdout, "choose_new_report") || !strings.Contains(detailed.stdout, "recover_report") {
		t.Fatal("explicit full diagnostic query cannot recover original remedies")
	}
	if sha256.Sum256([]byte(readFile(t, path))) != before {
		t.Fatal("query limit rejection changed saved evidence")
	}
}
