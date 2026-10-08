// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package publish

import "testing"

func TestPDFBoundaryHookRunsOnlyAfterVisibility(t *testing.T) {
	t.Parallel()
	fixture := newCommitFixture(t, realOperations())
	discardStaged(t, fixture.reportStage)
	pdf := fixture.pdf()
	calls := 0
	pdf.AfterCommit = func() { calls++; requirePublishedContent(t, fixture.pdfTarget, pdfContent) }

	result, err := Commit(t.Context(), pdf, nil, Policy{})
	if err != nil || !result.PDFPublished || calls != 1 {
		t.Fatalf("commit hook: %+v %v calls=%d", result, err, calls)
	}

	result, err = Commit(t.Context(), pdf, nil, Policy{})
	if err == nil || result.PDFPublished || calls != 1 {
		t.Fatalf("failed commit invoked hook: %+v %v calls=%d", result, err, calls)
	}
}

func TestPDFBoundaryHookRunsWhenDirectoryDurabilityFailsAfterVisibility(t *testing.T) {
	t.Parallel()

	ops := realOperations()
	fixture := newCommitFixture(t, ops)
	discardStaged(t, fixture.reportStage)

	ops.syncDirectory = func(string) error { return errInjected }
	pdf := fixture.pdf()
	calls := 0
	pdf.AfterCommit = func() { calls++; requirePublishedContent(t, fixture.pdfTarget, pdfContent) }

	result, err := commitWith(t.Context(), ops, pdf, nil, Policy{})
	if err == nil || !result.PDFPublished || calls != 1 {
		t.Fatalf("visible durability-failed hook: %+v %v calls=%d", result, err, calls)
	}
}
