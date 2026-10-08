// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package cli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/cli"
)

func TestUnknownFlagRecoveryUsesOnlyUnambiguousBoundedSuggestions(t *testing.T) {
	t.Parallel()

	cases := []struct{ command, flag, replacement string }{
		{argCheck, "--" + strings.Repeat("q", 40), ""},
		{argReport, "--pare", ""},
		{argCheck, "--pla", planOption},
		{argCheck, "--jlbs", argJobs},
	}
	for _, test := range cases {
		t.Run(test.flag, func(t *testing.T) {
			t.Parallel()

			_, err := cli.Parse([]string{test.command, test.flag})

			usage, ok := errors.AsType[*cli.UsageError](err)
			if !ok || usage.Diagnostics[0].Code != cli.CodeUnknownOption {
				t.Fatalf("unknown flag was not rejected: %v", err)
			}

			assertFlagRecovery(t, usage, test.replacement)
		})
	}
}

func assertFlagRecovery(t *testing.T, usage *cli.UsageError, replacement string) {
	t.Helper()

	recovery := usage.Diagnostics[0].Recovery
	if recovery == nil || recovery.Replacement != replacement {
		t.Fatalf("unsafe flag suggestion: %+v", recovery)
	}

	if replacement == "" && recovery.Action != "open_help" {
		t.Fatal("an ambiguous or overlong flag must use command help")
	}

	if replacement != "" &&
		(recovery.Action != "edit_input" || recovery.Location.ArgvIndex == nil || *recovery.Location.ArgvIndex != 1) {
		t.Fatal("correction lost original argv declaration")
	}
}

func TestUnknownFlagSuggestsOnlyActualAdjacentTranspositions(t *testing.T) {
	t.Parallel()

	cases := []struct{ flag, replacement string }{
		{"--paln", planOption},
		{"--nlap", ""},
		{"--plxy", ""},
		{"--xxxx", ""},
	}

	for _, test := range cases {
		t.Run(test.flag, func(t *testing.T) {
			t.Parallel()

			_, err := cli.Parse([]string{argCheck, test.flag})

			usage, ok := errors.AsType[*cli.UsageError](err)
			if !ok || usage.Diagnostics[0].Code != cli.CodeUnknownOption {
				t.Fatalf("unknown flag was not rejected: %v", err)
			}

			assertFlagRecovery(t, usage, test.replacement)
		})
	}
}
