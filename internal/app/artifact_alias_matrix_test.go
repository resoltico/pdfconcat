// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package app_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type artifactScenario struct{ populated, overwrite bool }

func TestArtifactAliasVariantsPreservePDFBytes(t *testing.T) {
	t.Parallel()

	names := [][2]string{{outputFile, outputFile}, {"case.pdf", "CASE.pdf"}, {"é.pdf", "é.pdf"}}

	scenarios := []artifactScenario{{}, {true, false}, {false, true}, {true, true}}
	for _, pair := range names {
		for _, scenario := range scenarios {
			name := fmt.Sprintf("%s/%s/populated=%t/overwrite=%t", pair[0], pair[1], scenario.populated, scenario.overwrite)
			t.Run(name, func(t *testing.T) { t.Parallel(); exerciseArtifactPair(t, pair, scenario) })
		}
	}
}

func exerciseArtifactPair(t *testing.T, pair [2]string, scenario artifactScenario) {
	t.Helper()
	dir := workDir(t)
	writePDF(t, dir, sourceA)
	source := readFile(t, filepath.Join(dir, sourceA))
	output, reportPath := filepath.Join(dir, pair[0]), filepath.Join(dir, pair[1])

	distinct := false
	if scenario.populated {
		distinct = populateArtifactPair(t, output, reportPath, source)
	}

	args := []string{commandBuild, outputFlag, pair[0], reportFlag, pair[1], sourceA}
	if scenario.overwrite {
		args = append(args, overwriteFlag)
	}

	result := execute(t.Context(), t, appOf(newFake(t)), dir, args...)
	if scenario.populated && distinct && scenario.overwrite {
		result.requireCode(t, 0, "")
		assertArtifactTypes(t, output, reportPath)
	} else {
		result.requireCode(t, 2, "")
		assertPreservedOutput(t, output, reportPath, source, scenario)
	}

	if readFile(t, filepath.Join(dir, sourceA)) != source {
		t.Fatal("source PDF bytes changed")
	}
}

func populateArtifactPair(t *testing.T, output, reportPath, source string) bool {
	t.Helper()
	writeFile(t, output, source)

	outputInfo, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}

	reportInfo, err := os.Stat(reportPath)

	distinct := os.IsNotExist(err) || (err == nil && !os.SameFile(outputInfo, reportInfo))
	if distinct {
		writeFile(t, reportPath, "existing report")
	}

	return distinct
}

func assertArtifactTypes(t *testing.T, output, reportPath string) {
	t.Helper()

	if !strings.HasPrefix(readFile(t, output), "%PDF-") || !strings.HasPrefix(readFile(t, reportPath), "{") {
		t.Fatal("artifact types were corrupted")
	}
}

func assertPreservedOutput(t *testing.T, output, reportPath, source string, scenario artifactScenario) {
	t.Helper()

	if scenario.populated {
		if readFile(t, output) != source {
			t.Fatal("original output PDF bytes changed")
		}

		return
	}

	requireMissing(t, output)
	requireMissing(t, reportPath)
}
