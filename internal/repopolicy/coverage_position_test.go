// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// The profile below is the physical output of a native `go test -coverprofile` control:
// Wrong(false) leaves its true branch uncovered; Trusted(false) and Trusted(true) cover both branches.
const (
	mappedCoverageSource = "package pkg\n\nfunc Wrong(ready bool) int {\n" +
		"\tif ready {\n\t\treturn 1\n\t}\n\treturn 2\n}\n\n" +
		"//line file.go:3\nfunc Trusted(ready bool) int {\n" +
		"\tif ready {\n\t\treturn 1\n\t}\n\treturn 2\n}\n"

	mappedCoverageProfile = "mode: set\n" +
		coverageModule + "/pkg/file.go:4.2,4.11 1 1\n" +
		coverageModule + "/pkg/file.go:5.3,6.1 1 0\n" +
		coverageModule + "/pkg/file.go:7.2,7.10 1 1\n" +
		coverageModule + "/pkg/file.go:12.2,12.11 1 1\n" +
		coverageModule + "/pkg/file.go:13.3,14.1 1 1\n" +
		coverageModule + "/pkg/file.go:15.2,15.10 1 1\n"
)

func TestCoverageDisplayMappingCannotExceptAnotherFunctionsBranch(t *testing.T) {
	t.Parallel()

	result := evaluateMappedCoverage(t, "Trusted")
	if result.Excluded != 0 || !strings.Contains(strings.Join(result.Problems, "\n"), "is executed by the tests") {
		t.Fatalf("display mapping transferred the exception to another function: %+v", result)
	}
}

func TestCoveragePhysicalFunctionScopeStillAcceptsItsUnexecutedBranch(t *testing.T) {
	t.Parallel()

	result := evaluateMappedCoverage(t, "Wrong")
	if len(result.Problems) != 0 || result.Excluded != 1 || result.Adjusted.Percent() != 100 {
		t.Fatalf("physical exception lost its exact branch: %+v", result)
	}
}

func evaluateMappedCoverage(t *testing.T, function string) repopolicy.CoverageResult {
	t.Helper()

	entry := &repopolicy.Entry{Tool: repopolicy.ToolCoverage, Path: coveragePath, Function: function, Anchor: "return 1"}

	return repopolicy.EvaluateCoverage(repopolicy.CoverageInput{
		Profile: parseProfile(t, mappedCoverageProfile), Entries: []*repopolicy.Entry{entry},
		Read:   sourceFrom(map[string]string{coveragePath: mappedCoverageSource}),
		Module: coverageModule, GOOS: "linux", Threshold: 100,
	})
}
