// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestDirectPlanWithNoAdmissiblePathRetainsBothFindings(t *testing.T) {
	_, ds := directPlan(Items{SourceItem{Ref: "source", Path: ""}}, t.TempDir())
	if len(ds) != 2 || ds[0].Code != diag.PathInvalid || ds[1].Code != diag.PlanValue {
		t.Fatalf("direct item refusal changed declaration diagnostics: %v", ds)
	}
}
