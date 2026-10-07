// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/publish"
	"github.com/resoltico/pdfconcat/internal/report"
)

func TestRecoveryRefreshPublicationResultKeepsCommittedMetadataCurrent(t *testing.T) {
	t.Parallel()

	current := &pipeline{publication: report.Publication{RecoveryState: report.RecoveryCurrent}}
	if err := current.finishRecoveryRefresh(nil); err != nil || len(current.warnings) != 0 {
		t.Fatalf("successful refresh: %v warnings=%v", err, current.warnings)
	}

	failed := fmt.Errorf("refresh: %w", errInjected)
	if err := current.finishRecoveryRefresh(failed); !errors.Is(err, errInjected) || len(current.warnings) != 0 {
		t.Fatalf("lost ordinary publication failure: %v warnings=%v", err, current.warnings)
	}

	finalization := fmt.Errorf("refresh: %w", &publish.FinalizationError{Path: "/recovery.json", Err: errInjected})

	refreshErr := current.finishRecoveryRefresh(finalization)
	if refreshErr != nil || current.publication.RecoveryState != report.RecoveryCurrent ||
		len(current.warnings) != 1 {
		t.Fatalf(
			"committed recovery falsely failed: %v state=%s warnings=%v",
			refreshErr,
			current.publication.RecoveryState,
			current.warnings,
		)
	}

	warning := current.warnings[0]
	if !strings.Contains(warning, errInjected.Error()) || !strings.Contains(warning, "recovery metadata is visible") {
		t.Fatalf("lost committed visibility or finalization caveat: %s", warning)
	}
}
