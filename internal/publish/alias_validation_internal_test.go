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

func TestCommitRejectsIdenticalReportAndPDFTargetsBeforeReplacingBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	output := filepath.Join(dir, "shared.pdf")
	stage := filepath.Join(dir, "pending.pdf")

	const before = "existing PDF"
	if err := os.WriteFile(output, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(stage, []byte("replacement PDF"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Stage(t.Context(), output, strings.NewReader(`{"kind":"report"}`), 1000)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Commit(t.Context(), PDF{Staged: stage, Destination: output, Overwrite: true}, report, Policy{Overwrite: true})

	invalid, ok := errors.AsType[*DestinationError](err)
	if !ok || invalid.Subject != "report" || result.PDFPublished || result.ReportPublished {
		t.Fatalf("same-path publication: %+v %v", result, err)
	}

	requirePublishedContent(t, output, before)

	if _, statErr := os.Stat(report.Path()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected report staging not removed: %v", statErr)
	}
}

func TestAliasChecksRejectFilesystemInspectionErrors(t *testing.T) {
	t.Parallel()

	for _, paths := range [][2]string{{"bad\x00name", "report.json"}, {"output.pdf", "bad\x00name"}} {
		if err := rejectArtifactAlias(paths[0], paths[1]); err == nil {
			t.Fatal("filesystem inspection error accepted")
		}
	}
}

func TestReportPublicationCompletesAfterPDFCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ops := realOperations()
	fixture := newCommitFixture(t, ops)
	ops.syncDirectory = func(string) error { cancel(); return nil }

	result, err := commitWith(ctx, ops, fixture.pdf(), fixture.reportStage, Policy{})
	if err != nil || !result.PDFPublished || !result.ReportPublished {
		t.Fatalf("post-PDF cancellation: %+v %v", result, err)
	}
}

func TestDestinationProtectionRechecksAfterVerification(t *testing.T) {
	t.Parallel()
	fixture := newCommitFixture(t, realOperations())
	discardStaged(t, fixture.reportStage)
	pdf := fixture.pdf()

	const concurrentContent = "created by another writer during verification"

	pdf.Verify = func() error { return os.WriteFile(fixture.pdfTarget, []byte(concurrentContent), 0o600) }

	result, err := Commit(t.Context(), pdf, nil, Policy{})
	if err == nil || result.PDFPublished {
		t.Fatalf("late destination bypassed protection: %+v %v", result, err)
	}

	requirePublishedContent(t, fixture.pdfTarget, concurrentContent)
}
