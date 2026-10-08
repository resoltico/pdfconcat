// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedReportRemainsVisibleWhenIdentityPinCloseFails(t *testing.T) {
	t.Parallel()

	staged, stageErr := Stage(t.Context(), filepath.Join(t.TempDir(), reportPath), strings.NewReader(reportContent), 1000)
	if stageErr != nil {
		t.Fatal(stageErr)
	}

	originalReplace := staged.ops.replace

	staged.ops.replace = func(source, target string, policy existingFile) error {
		if err := staged.owner.file.Close(); err != nil {
			t.Fatal(err)
		}

		return originalReplace(source, target, policy)
	}
	if err := staged.Publish(t.Context(), Policy{}); err != nil {
		t.Fatalf("cleanup rolled back visibility: %v", err)
	}

	if !errors.Is(staged.CleanupError(), os.ErrClosed) {
		t.Fatalf("cleanup warning lost: %v", staged.CleanupError())
	}

	if get(t, staged.Target()) != reportContent {
		t.Fatal("committed report changed")
	}
}

func TestCommitCarriesReportCleanupWarningWithoutChangingCommittedOutcome(t *testing.T) {
	t.Parallel()
	fixture := newCommitFixture(t, realOperations())
	originalReplace := fixture.reportStage.ops.replace
	fixture.reportStage.ops.replace = func(source, target string, policy existingFile) error {
		if err := fixture.reportStage.owner.file.Close(); err != nil {
			t.Fatal(err)
		}

		return originalReplace(source, target, policy)
	}

	result, err := Commit(t.Context(), fixture.pdf(), fixture.reportStage, Policy{})
	if err != nil || !result.PDFPublished || !result.ReportPublished {
		t.Fatalf("cleanup changed committed outcome: %+v %v", result, err)
	}

	if !errors.Is(result.ReportCleanupError, os.ErrClosed) {
		t.Fatalf("result warning lost: %v", result.ReportCleanupError)
	}

	if get(t, fixture.pdfTarget) != pdfContent || get(t, fixture.reportPath) != reportContent {
		t.Fatal("cleanup changed published bytes")
	}
}
