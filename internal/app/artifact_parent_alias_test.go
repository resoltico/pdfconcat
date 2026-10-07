// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactPathsThroughSymlinkedParentsCannotAlias(t *testing.T) {
	t.Parallel()

	for _, scenario := range []artifactScenario{{}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprintf("populated=%t/overwrite=%t", scenario.populated, scenario.overwrite), func(t *testing.T) {
			t.Parallel()
			dir := workDir(t)
			writePDF(t, dir, sourceA)
			source := readFile(t, filepath.Join(dir, sourceA))

			actual, alias := filepath.Join(dir, "actual"), filepath.Join(dir, "parent-alias")
			if err := os.Mkdir(actual, 0o700); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink(actual, alias); err != nil {
				t.Fatalf("required symlinked-parent filesystem capability unavailable: %v", err)
			}

			output, reportPath := filepath.Join(actual, outputFile), filepath.Join(alias, outputFile)
			if scenario.populated {
				writeFile(t, output, source)
			}

			args := []string{commandBuild, outputFlag, output, reportFlag, reportPath, sourceA}
			if scenario.overwrite {
				args = append(args, overwriteFlag)
			}

			execute(t.Context(), t, appOf(newFake(t)), dir, args...).requireCode(t, 2, "")
			assertPreservedOutput(t, output, reportPath, source, scenario)
		})
	}
}

func TestCheckRechecksLateReportSourceAlias(t *testing.T) {
	t.Parallel()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	source := filepath.Join(dir, sourceA)
	before := readFile(t, source)
	reportPath := filepath.Join(dir, reportFile)
	runner := appOf(newFake(t))
	runner.BeforeCommit(func() {
		if err := os.Link(source, reportPath); err != nil {
			t.Fatal(err)
		}
	})
	execute(t.Context(), t, runner, dir, commandCheck, reportFlag, reportFile, overwriteFlag, sourceA).requireCode(t, 2, "alias_conflict")

	if readFile(t, source) != before || readFile(t, reportPath) != before {
		t.Fatal("late source alias changed original PDF bytes")
	}
}
