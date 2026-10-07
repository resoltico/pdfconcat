// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/publish"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestCommittedReportCleanupWarningPreservesPublishedStateAndIndependentDurabilityWarning(t *testing.T) {
	t.Parallel()

	file, createErr := os.CreateTemp(t.TempDir(), "cleanup-*")
	if createErr != nil {
		t.Fatal(createErr)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	closeErr := file.Close()
	if !errors.Is(closeErr, os.ErrClosed) {
		t.Fatalf("actual close failure prerequisite: %v", closeErr)
	}

	current := &pipeline{output: filepath.Join(t.TempDir(), "output.pdf"), reportPath: filepath.Join(t.TempDir(), "report.json")}
	result := &publish.Result{PDFPublished: true, ReportPublished: true, ReportCleanupError: closeErr}

	durability := &publish.DurabilityError{Path: current.reportPath, Err: os.ErrPermission}
	if err := current.afterCommit(t.Context(), result, durability); err != nil {
		t.Fatal(err)
	}

	if !current.publication.Published || current.publication.ReportStatus != report.ReportWritten {
		t.Fatalf("cleanup undid publication: %+v", current.publication)
	}

	if len(current.warnings) != 2 || current.warnings[0] != closeErr.Error() ||
		!strings.Contains(current.warnings[1], "may not survive a crash") {
		t.Fatalf("independent warnings lost: %v", current.warnings)
	}
}
