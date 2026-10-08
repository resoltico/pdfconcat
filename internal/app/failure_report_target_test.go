// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestUnusablePDFTargetStillSavesCausalFailureReport(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	parentFile := filepath.Join(dir, "parent-file")
	writeFile(t, parentFile, "not a directory")

	targets := []string{dir, filepath.Join(dir, "missing-parent", outputFile), filepath.Join(parentFile, "child", outputFile)}
	for index, target := range targets {
		path := filepath.Join(dir, "evidence-"+string(rune('a'+index))+".json")
		res := execute(t.Context(), t, appOf(newFake(t)), dir, commandBuild, outputFlag, target, reportFlag, path, sourceA)
		res.requireCode(t, 2, "")

		assertTargetFailureEvidence(t, path, target)
	}
}

func assertTargetFailureEvidence(t *testing.T, path, target string) {
	t.Helper()

	saved, err := report.Decode(t.Context(), path, strings.NewReader(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}

	if len(saved.Diagnostics) != 1 || saved.Publication.Published || saved.Publication.ReportStatus != report.ReportWritten {
		t.Fatalf("causal report: %+v", saved)
	}

	fault := saved.Diagnostics[0]
	if fault.Path != target || fault.Location == nil || fault.Recovery == nil || fault.Recovery.Action != "edit_input" {
		t.Fatalf("wrong repair target: %+v", fault)
	}

	if fault.Severity != report.SeverityError || saved.ErrorCount != 1 || saved.WarningCount != 0 {
		t.Fatalf("fault counts: %+v", saved)
	}
}
