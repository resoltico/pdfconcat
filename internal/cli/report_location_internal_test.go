// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestReportValidationLocatesEveryRequestFaultAndSelectorOrder(t *testing.T) {
	type faultCase struct {
		arguments string
		index     int
		code      report.Code
	}
	cases := []faultCase{
		{"report --details r", 1, report.CodeDetailsNeedSelect},
		{"report --offset=1 r", 1, report.CodePagingNeedsView},
		{"report --limit=2 r", 1, report.CodePagingNeedsView},
		{"report --view=invalid r", 1, report.CodeUnknownView},
		{"report --page=0 r", 1, report.CodeInvalidNumber},
		{"report --expect-attempt=invalid r", 1, report.CodeInvalidExpectation},
		{"report --view=parts --offset=-1 r", 2, report.CodeInvalidPaging},
		{"report --view=parts --limit=101 r", 2, report.CodeInvalidPaging},
	}
	selectors := []string{"--part=x", "--page=1", "--view=parts"}
	for _, first := range selectors {
		for _, second := range selectors {
			if first == second {
				continue
			}
			cases = append(cases, faultCase{"report " + first + " " + second + " r", 2, report.CodeSelectionConflict}, faultCase{"report --help " + first + " " + second, 3, report.CodeSelectionConflict})
		}
	}
	for _, tc := range cases {
		t.Run(tc.arguments, func(t *testing.T) {
			_, err := Parse(strings.Fields(tc.arguments))
			var failure *UsageError
			if !errors.As(err, &failure) || len(failure.Diagnostics) != 1 {
				t.Fatalf("request fault=%v", err)
			}
			if failure.Diagnostics[0].Code != tc.code {
				t.Fatalf("code=%s want=%s", failure.Diagnostics[0].Code, tc.code)
			}
			want := fmt.Sprintf("argv:%d", tc.index)
			if failure.Diagnostics[0].Location == nil || failure.Diagnostics[0].Location.ArgvIndex == nil || *failure.Diagnostics[0].Location.ArgvIndex != tc.index {
				t.Fatalf("location=%+v want=%s", failure.Diagnostics[0].Location, want)
			}
		})
	}
}
