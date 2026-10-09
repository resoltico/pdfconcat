// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package cli_test

import (
	"testing"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/cli"
)

func TestFitAndProgressPlanningGrammar(t *testing.T) {
	t.Parallel()

	for _, command := range []string{argBuild, argCheck} {
		for _, paper := range []assembly.FitTarget{assembly.FitA4, assembly.FitLegal} {
			for _, mode := range []cli.ProgressMode{cli.ProgressAuto, cli.ProgressJSON, cli.ProgressNone} {
				parsed, err := cli.Parse([]string{command, argBlank, argFitTo, string(paper), "--progress=" + string(mode)})
				if err != nil || parsed.FitTo != paper || parsed.Progress != mode {
					t.Fatalf("%s %s %s: %+v %v", command, paper, mode, parsed, err)
				}
			}
		}
	}
}

func TestFitAndProgressRejectUnsupportedOptionsBeforeWork(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{argCheck, argBlank, argFitTo, "Letter"},
		{argBuild, argBlank, argFitTo, "a4"},
		{argCheck, argBlank, argProgress, "JSON"},
		{argBuild, argBlank, argProgress, ""},
		{argCheck, argBlank, argProgressJSON, "--progress=none"},
		{"report", "missing.json", argProgressJSON},
		{argSchemaCommand, "response", argProgressJSON},
		{argVersionCommand, argFitTo, "A4"},
	} {
		if _, err := cli.Parse(args); err == nil {
			t.Fatalf("unsupported grammar admitted: %v", args)
		}
	}
}
