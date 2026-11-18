// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestDestinationPolicyUsesKnowledgeAndObservedIdentity(t *testing.T) {
	for _, tc := range []struct {
		role             DestinationRole
		known, overwrite bool
		want             diag.Code
		remedy           string
	}{
		{ReportDestination, false, true, diag.ReportExists, "Choose a new report path"},
		{ReportDestination, true, false, diag.ReportExists, "--overwrite"},
		{OutputDestination, true, false, diag.OutputExists, "--overwrite"},
	} {
		ds := CheckDestination(DestinationFacts{Path: "destination", Role: tc.role, Exists: true, Regular: true, InputsKnown: tc.known, Overwrite: tc.overwrite})
		if len(ds) != 1 || ds[0].Code != tc.want || !strings.Contains(ds[0].Message, tc.remedy) ||
			(strings.Contains(ds[0].Message, "--overwrite") && tc.remedy != "--overwrite") {
			t.Fatalf("replacement rule: %+v", ds)
		}
	}
	if ds := CheckDestination(DestinationFacts{Path: "target", Exists: true}); len(ds) != 1 || ds[0].Code != diag.OutputNotRegular || strings.Contains(ds[0].Message, "--overwrite") {
		t.Fatal(ds)
	}
	if ds := CheckDestination(DestinationFacts{Path: "destination", Exists: true, Regular: true, Overwrite: true, AliasedInput: "font"}); len(ds) != 1 || ds[0].Code != diag.OutputIsInput {
		t.Fatal(ds)
	}
	if !MayReplace(OutputDestination, true, false) || MayReplace(ReportDestination, true, false) || !MayReplace(ReportDestination, true, true) {
		t.Fatal("commit replacement differs from admission rule")
	}
}
