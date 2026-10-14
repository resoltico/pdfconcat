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

	report, err := Stage(StageTestContext(t), output, readerRequiringStorage(strings.NewReader(`{"kind":"report"}`)), 1000)
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

func TestArtifactAliasIdentityRequiresTwoExistingMatchingObjects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	output, report := filepath.Join(dir, "output.pdf"), filepath.Join(dir, "report.json")
	if err := rejectArtifactAlias(output, report); err != nil {
		t.Fatalf("two absent destinations confused with matching objects: %v", err)
	}
	if err := os.WriteFile(output, []byte(pdfContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rejectArtifactAlias(output, report); err != nil {
		t.Fatalf("absent report destination confused with PDF identity: %v", err)
	}
	if err := os.Link(output, report); err != nil {
		t.Fatal(err)
	}
	if alias, ok := errors.AsType[*DestinationError](rejectArtifactAlias(output, report)); !ok || alias.Subject != "report" {
		t.Fatal("hard-linked destinations did not produce an alias refusal")
	}
}

func TestReportPublicationChecksLateAliasAndOriginalVerification(t *testing.T) {
	t.Parallel()
	for _, lateAlias := range []bool{false, true} {
		fixture := newCommitFixture(t, realOperations())
		pdf := fixture.pdf()
		calls := 0
		fixture.reportStage.Verify = func() error { calls++; return errInjected }
		if lateAlias {
			pdf.AfterCommit = func() {
				if err := os.Link(fixture.pdfTarget, fixture.reportStage.target); err != nil {
					t.Fatal(err)
				}
			}
		}
		result, err := Commit(t.Context(), pdf, fixture.reportStage, Policy{Overwrite: true})
		if !result.PDFPublished || result.ReportPublished || err == nil {
			t.Fatalf("post-commit protection lateAlias=%v: %+v %v", lateAlias, result, err)
		}
		if lateAlias {
			if alias, ok := errors.AsType[*DestinationError](err); !ok || alias.Subject != "report" || calls != 0 {
				t.Fatalf("late alias did not stop before original verification: %v, calls=%d", err, calls)
			}
		} else if !errors.Is(err, errInjected) || calls != 1 {
			t.Fatalf("original report verification bypassed: %v, calls=%d", err, calls)
		}
		requirePublishedContent(t, fixture.pdfTarget, pdfContent)
	}
}
