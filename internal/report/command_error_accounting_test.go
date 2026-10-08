// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report_test

import (
	"strconv"
	"testing"

	"github.com/resoltico/pdfconcat/internal/report"
)

func TestCommandFailurePreviewAccountsForEveryDiagnostic(t *testing.T) {
	t.Parallel()

	for _, count := range []int{0, 1, 3, 8} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()

			diagnostics := make([]report.Diagnostic, count)
			for index := range diagnostics {
				diagnostics[index] = report.Diagnostic{Stage: "instructions", Code: "bad_flag", Message: "fault " + strconv.Itoa(index)}
			}

			fault := report.NewCommandError(testCommandCheck, report.StatusInvalid, diagnostics, "/pdfconcat")
			if fault.DiagnosticsOmitted < 0 || len(fault.Diagnostics)+fault.DiagnosticsOmitted != count {
				t.Fatalf(
					"command failure lost diagnostic accounting: shown=%d omitted=%d input=%d",
					len(fault.Diagnostics),
					fault.DiagnosticsOmitted,
					count,
				)
			}

			for index, diagnostic := range fault.Diagnostics {
				if diagnostic.Message != diagnostics[index].Message {
					t.Fatalf("command preview reordered fault %d: %q", index, diagnostic.Message)
				}
			}
		})
	}
}
