// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCancellationDuringVerificationOrFlushPreservesDestination(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"verify", "sync"} {
		t.Run(phase, func(t *testing.T) { t.Parallel(); exerciseCanceledCommit(t, phase) })
	}
}

func exerciseCanceledCommit(t *testing.T, phase string) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ops := realOperations()
	fixture := newCommitFixture(t, ops)

	const originalPDF = "original PDF"
	if err := os.WriteFile(fixture.pdfTarget, []byte(originalPDF), 0o600); err != nil {
		t.Fatal(err)
	}

	pdf := fixture.pdf()

	pdf.Overwrite = true
	if phase == "verify" {
		pdf.Verify = func() error { cancel(); return nil }
	} else {
		ops.syncFile = func(path string) error { err := syncFile(path); cancel(); return err }
	}

	result, err := commitWith(ctx, ops, pdf, fixture.reportStage, Policy{})
	if !errors.Is(err, context.Canceled) || result.PDFPublished {
		t.Fatalf("commit: %+v %v", result, err)
	}

	requirePublishedContent(t, fixture.pdfTarget, originalPDF)
}

func TestLateReportAliasKeepsPublishedPDFAndRecovery(t *testing.T) {
	t.Parallel()

	for _, alias := range []string{"case", "unicode", hardlinkVariant} {
		t.Run(alias, func(t *testing.T) { t.Parallel(); exerciseLateAlias(t, alias) })
	}
}

func exerciseLateAlias(t *testing.T, alias string) {
	t.Helper()
	dir := t.TempDir()
	output, target := lateAliasPaths(dir, alias)
	stage := filepath.Join(dir, stagedPDFPath)

	const (
		pdfContent    = "%PDF-real-content"
		reportContent = `{"kind":"report"}`
	)

	if err := os.WriteFile(stage, []byte(pdfContent), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Stage(t.Context(), target, strings.NewReader(reportContent), 1000)
	if err != nil {
		t.Fatal(err)
	}

	ops := realOperations()
	ops.syncDirectory = func(string) error {
		if alias == hardlinkVariant {
			return os.Link(output, target)
		}

		return nil
	}
	result, err := commitWith(t.Context(), ops, PDF{Staged: stage, Destination: output}, report, Policy{Overwrite: true})
	info, pdfErr := os.Stat(output)
	other, reportErr := os.Stat(target)

	aliases := pdfErr == nil && reportErr == nil && os.SameFile(info, other)
	if aliases {
		requireRecovery(t, &result, err, reportContent)
	} else if err != nil || !result.ReportPublished {
		t.Fatalf("distinct commit: %+v %v", result, err)
	}

	requirePublishedContent(t, output, pdfContent)
}

func lateAliasPaths(dir, alias string) (string, string) {
	switch alias {
	case "unicode":
		return filepath.Join(dir, "café.pdf"), filepath.Join(dir, "café.pdf")
	case hardlinkVariant:
		return filepath.Join(dir, outputPath), filepath.Join(dir, reportPath)
	default:
		return filepath.Join(dir, outputPath), filepath.Join(dir, "OUT.pdf")
	}
}

func requireRecovery(t *testing.T, result *Result, err error, content string) {
	t.Helper()
	t.Cleanup(func() {
		if closeErr := result.CloseRecovery(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	if !result.PDFPublished || result.ReportPublished || result.RecoveryPath == "" || err == nil {
		t.Fatalf("alias commit: %+v %v", result, err)
	}

	requirePublishedContent(t, result.RecoveryPath, content)
}

func requirePublishedContent(t *testing.T, path, content string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil || string(data) != content {
		t.Fatalf("published content: %q %v", data, err)
	}
}
