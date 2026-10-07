// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/pdfengine"
)

type reportStagingScenario struct {
	name, command, sourceContent, inspectionState string
	diagnostics                                   int
}

func TestReportStagingDenialPreservesInputsAndExistingReport(t *testing.T) {
	t.Parallel()

	for _, test := range []reportStagingScenario{
		{"assembly", commandBuild, "", phaseComplete, 1},
		{"check", commandCheck, "", phaseComplete, 1},
		{"failed inspection", commandCheck, notAPDF, phaseIncomplete, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkReportStagingDenial(t, test)
		})
	}
}

func checkReportStagingDenial(t *testing.T, test reportStagingScenario) {
	t.Helper()
	requireDirectoryPermissions(t)
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	if test.sourceContent != "" {
		writeFile(t, filepath.Join(dir, sourceA), test.sourceContent)
	}

	source := readFile(t, filepath.Join(dir, sourceA))

	reports := filepath.Join(dir, "sealed-reports")
	if err := os.Mkdir(reports, 0o700); err != nil {
		t.Fatal(err)
	}

	reportPath := filepath.Join(reports, reportFile)
	writeFile(t, reportPath, previousReportContent)
	engine := newFake(t)
	engine.inspect = func(ctx context.Context, path string) (pdfengine.SourceInfo, error) {
		makeReadOnly(t, reports)
		return engine.realInspect(ctx, path)
	}

	args := []string{test.command, reportFlag, reportPath, overwriteFlag, detailsFlag, sourceA}
	if test.command == commandBuild {
		args = append(args, outputFlag, outputFile)
	}

	result := execute(t.Context(), t, appOf(engine), dir, args...)

	parsed := result.requireCode(t, 1, reportWriteFailureCode)
	if parsed.Publication.Published || parsed.Publication.ReportStatus != failedState {
		t.Fatalf("staging denial publication: %+v", parsed.Publication)
	}

	if len(parsed.Diagnostics) != test.diagnostics || parsed.Phases["input_inspection"] != test.inspectionState {
		t.Fatalf("staging failure lost its inspection outcome: %+v", parsed)
	}

	if readFile(t, reportPath) != previousReportContent || readFile(t, filepath.Join(dir, sourceA)) != source {
		t.Fatal("staging failure changed an existing artifact or input")
	}

	requireMissing(t, filepath.Join(dir, outputFile))
}

func TestWorkspaceCreationDenialPublishesNothing(t *testing.T) {
	t.Parallel()
	requireDirectoryPermissions(t)
	dir := workDir(t)
	writePDF(t, dir, sourceA)

	outDir := filepath.Join(dir, "sealed-output")
	if err := os.Mkdir(outDir, 0o700); err != nil {
		t.Fatal(err)
	}

	makeReadOnly(t, outDir)
	output := filepath.Join(outDir, outputFile)
	result := execute(t.Context(), t, appOf(newFake(t)), dir, commandBuild, outputFlag, output, detailsFlag, sourceA)

	parsed := result.requireCode(t, 1, "scratch_failed")
	if parsed.Publication.Published {
		t.Fatal("workspace creation failure published an output")
	}

	requireMissing(t, output)
}
