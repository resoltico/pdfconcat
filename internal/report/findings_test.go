// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestComputedTextFindingsRoundTripAndRejectInvalidGeometry(t *testing.T) {
	t.Parallel()

	valid := []report.TextFinding{
		{Kind: "word-wider-than-box", Line: 0, Detail: "wide word"},
		{Kind: findingOutsideHorizontal, Line: -1, Detail: "ink leaves page"},
		{Kind: "outside-page-vertical", Line: -1, Detail: "mark above page"},
	}
	for _, findings := range [][]report.TextFinding{valid, nil} {
		value := mustDecode(t, completeCheck)
		value.Styles[0].Text.Findings = findings

		decoded := mustDecode(t, encodeReport(t, value))
		if len(decoded.Styles[0].Text.Findings) != len(findings) {
			t.Fatal("findings lost during report round trip")
		}
	}

	for _, finding := range []report.TextFinding{
		{Kind: unknownFindingKind, Line: -1, Detail: "x"},
		{Kind: "word-wider-than-box", Line: -1, Detail: "x"},
		{Kind: findingOutsideHorizontal, Line: 0, Detail: "x"},
		{Kind: "outside-page-vertical", Line: -1},
	} {
		value := mustDecode(t, completeCheck)

		value.Styles[0].Text.Findings = []report.TextFinding{finding}
		if err := value.Validate(); err == nil {
			t.Fatalf("invalid text finding accepted: %+v", finding)
		}
	}

	value := mustDecode(t, completeCheck)

	value.Styles[0].Text.Findings = make([]report.TextFinding, report.MaxTextRunes+1)
	if err := value.Validate(); err == nil {
		t.Fatal("unbounded findings accepted")
	}
}
