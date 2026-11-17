// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestCompleteOutcomeExitMapping(t *testing.T) {
	for _, tc := range []struct {
		status diag.Status
		exit   int
	}{{diag.OK, 0}, {diag.StatusFailed, 1}, {diag.StatusInvalid, 2}, {diag.StatusInterrupted, 130}} {
		if got := cli.ExitCode(tc.status); got != tc.exit {
			t.Fatalf("%s: %d", tc.status, got)
		}
	}
}
