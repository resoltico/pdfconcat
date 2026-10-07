// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestReportProducerAndDigestRejectInvalidIdentity(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", strings.Repeat("a", 257)} {
		saved := mustDecode(t, completeCheck)

		saved.Producer = &report.Producer{
			Tool:     value,
			Version:  "devel",
			Commit:   unknownProducerCommit,
			Go:       "go1.27.1",
			Platform: "darwin/arm64",
		}
		if err := saved.Validate(); err == nil {
			t.Fatal("invalid producing tool accepted")
		}
	}

	saved := mustDecode(t, completeCheck)

	saved.Producer = &report.Producer{
		Tool:     strings.Repeat("ā", 256),
		Version:  "devel",
		Commit:   unknownProducerCommit,
		Go:       "go1.27.1",
		Platform: "darwin/arm64",
	}
	if err := saved.Validate(); err != nil {
		t.Fatalf("256-character identity: %v", err)
	}

	saved.Publication.OutputDigest = "not a digest"
	if err := saved.Validate(); err == nil {
		t.Fatal("invalid output digest accepted")
	}
}

func TestRecoveryReferenceRequiresTruthfulState(t *testing.T) {
	t.Parallel()

	for _, state := range []report.RecoveryState{report.RecoveryCurrent, report.RecoveryPending, report.RecoveryUnavailable} {
		saved := mustDecode(t, richFailure)

		saved.Publication.RecoveryState = state
		if err := saved.Validate(); err != nil {
			t.Fatalf("recovery state %s: %v", state, err)
		}
	}

	saved := mustDecode(t, richFailure)

	saved.Publication.RecoveryState = ""
	if err := saved.Validate(); err == nil {
		t.Fatal("unqualified recovery data accepted")
	}

	saved.Publication.RecoveryState = report.RecoveryCurrent

	saved.Publication.RecoveryReport = ""
	if err := saved.Validate(); err == nil {
		t.Fatal("recovery state without data accepted")
	}
}
