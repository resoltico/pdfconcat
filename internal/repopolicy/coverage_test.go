// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	coverageModule = "example.test/m"

	// coveragePath is the file the coverage fixtures describe, relative to the module.
	coveragePath = "pkg/file.go"

	coverageSource = "package pkg\n\n" + // Lines 1-2.
		"func Run() error {\n" + // Line 3.
		"\tif err := step(); err != nil {\n" + // Line 4.
		"\t\treturn err\n" + // Line 5.
		"\t}\n" + // Line 6.
		"\n" + // Line 7.
		"\treturn nil\n" + // Line 8.
		"}\n" // Line 9.

	// coverageProfile covers Run's success path (blocks 3-4 and 8) and leaves the error branch (4-6) unexecuted.
	coverageProfile = "mode: atomic\n" +
		"example.test/m/pkg/file.go:3.20,4.30 1 5\n" +
		"example.test/m/pkg/file.go:4.30,6.3 1 0\n" +
		"example.test/m/pkg/file.go:8.2,8.12 1 5\n" +
		"example.test/m/other/file.go:3.1,4.1 7 7\n"

	errorBranchEntry = `
  - id: coverage-run-error-branch
    tool: coverage
    kind: unreachable-branch
    path: pkg/file.go
    function: Run
    anchor: if err := step(); err != nil {
    retained_property: the error is returned to the caller
`

	windowsOnlyEntry = `
  - id: coverage-windows-only
    tool: coverage
    kind: unreachable-platform-branch
    goos: windows
    path: pkg/file_windows.go
    function: Run
    anchor: if err != nil {
    retained_property: the error is returned to the caller
`

	runnerSource = "package pkg\n\ntype Runner[T any] struct{}\n\nfunc (r *Runner[T]) Run() error {\n\tif fail() {\n" +
		"\t\treturn nil\n\t}\n\n\treturn nil\n}\n"

	runnerEntry = `
  - id: coverage-runner-guard
    tool: coverage
    kind: unreachable-branch
    path: pkg/file.go
    function: Runner.Run
    anchor: if fail() {
    retained_property: the guard is defensive
`
)

func parseProfile(t *testing.T, text string) *repopolicy.CoverageProfile {
	t.Helper()

	profile, err := repopolicy.ParseCoverageProfile(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}

	return profile
}

// evaluate applies the entries in body to a profile, on linux, against the standard source.
func evaluate(t *testing.T, profile, body string, threshold float64) repopolicy.CoverageResult {
	t.Helper()

	var entries []*repopolicy.Entry
	if body != "" {
		entries = sampleRegistry(t, body).Exceptions
	}

	return repopolicy.EvaluateCoverage(repopolicy.CoverageInput{
		Profile:   parseProfile(t, profile),
		Entries:   entries,
		Module:    coverageModule,
		GOOS:      testLinuxOS,
		Threshold: threshold,
		Read:      sourceFrom(map[string]string{coveragePath: coverageSource}),
	})
}

func TestEvaluateCoverageFailsBelowThresholdWithoutEntries(t *testing.T) {
	t.Parallel()

	// Raw: 10 statements, 9 covered.
	raw := evaluate(t, coverageProfile, "", 95)
	if raw.Raw.Total != 10 || raw.Raw.Covered != 9 || raw.Excluded != 0 {
		t.Fatalf("raw coverage %+v excluded %d", raw.Raw, raw.Excluded)
	}

	if len(raw.Problems) != 1 || !strings.Contains(raw.Problems[0], "below the threshold 95.00%") {
		t.Fatalf("raw run should fail the threshold: %q", raw.Problems)
	}
}

func TestEvaluateCoverageAppliesEntries(t *testing.T) {
	t.Parallel()

	// The entry removes the uncovered error branch's one statement.
	adjusted := evaluate(t, coverageProfile, errorBranchEntry, 95)
	if len(adjusted.Problems) != 0 {
		t.Fatalf(unexpectedProblemsFormat, adjusted.Problems)
	}

	if adjusted.Excluded != 1 || adjusted.Adjusted.Total != 9 || adjusted.Adjusted.Percent() != 100 {
		t.Fatalf("adjusted %+v excluded %d", adjusted.Adjusted, adjusted.Excluded)
	}

	if adjusted.Raw.Percent() >= adjusted.Adjusted.Percent() {
		t.Fatal("raw and adjusted coverage must be reported separately, with the exclusion visible in the difference")
	}

	if len(adjusted.Packages) != 2 || adjusted.Packages[0].Package != "other" || adjusted.Packages[1].Package != "pkg" {
		t.Fatalf("packages %+v", adjusted.Packages)
	}
}

// TestEvaluateCoverageRejectsStaleEntries is the negative control: an exception for code the tests
// do execute, for a renamed function, or for a vanished anchor fails the gate.
func TestEvaluateCoverageRejectsStaleEntries(t *testing.T) {
	t.Parallel()

	executed := strings.Replace(coverageProfile, "4.30,6.3 1 0", "4.30,6.3 1 3", 1)
	requireContains(t, evaluate(t, executed, errorBranchEntry, 50).Problems, "is executed by the tests; delete the exception")

	cases := map[string]string{
		"function renamed": mustReplace(t, errorBranchEntry, "function: Run", "function: Execute"),
		"anchor vanished":  mustReplace(t, errorBranchEntry, "anchor: if err := step(); err != nil {", "anchor: if err != nil {"),
	}

	for name, entry := range cases {
		if len(evaluate(t, coverageProfile, entry, 50).Problems) == 0 {
			t.Fatalf("%s: stale entry accepted", name)
		}
	}
}

func TestEvaluateCoverageSkipsOtherPlatformEntries(t *testing.T) {
	t.Parallel()

	result := evaluate(t, coverageProfile, windowsOnlyEntry, 50)
	if len(result.Problems) != 0 || len(result.NotEvaluated) != 1 {
		t.Fatalf("problems %q not-evaluated %q", result.Problems, result.NotEvaluated)
	}

	judged := repopolicy.EvaluateCoverage(repopolicy.CoverageInput{
		Profile:   parseProfile(t, coverageProfile),
		Entries:   sampleRegistry(t, windowsOnlyEntry).Exceptions,
		Module:    coverageModule,
		GOOS:      "windows",
		Threshold: 50,
		Read:      sourceFrom(nil),
	})
	requireContains(t, judged.Problems, "coverage-windows-only: cannot read pkg/file_windows.go")
}

func TestEvaluateCoverageEmptyProfileFails(t *testing.T) {
	t.Parallel()

	requireContains(t, evaluate(t, "mode: atomic\n", "", 90).Problems, "nothing was measured")
}

func TestParseCoverageProfileRejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no mode":         "example.test/m/a.go:1.1,2.1 1 1\n",
		"short line":      "mode: set\nexample.test/m/a.go:1.1,2.1 1\n",
		"bad position":    "mode: set\nexample.test/m/a.go:1,2.1 1 1\n",
		"non-numeric":     "mode: set\nexample.test/m/a.go:1.1,2.1 x 1\n",
		"missing comma":   "mode: set\nexample.test/m/a.go:1.1 1 1\n",
		"no colon at all": "mode: set\nnonsense 1 1\n",
	}

	for name, text := range cases {
		_, err := repopolicy.ParseCoverageProfile(strings.NewReader(text))
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestMergeProfilesSumsCountsAndRejectsModeMismatch(t *testing.T) {
	t.Parallel()

	unit := parseProfile(t, "mode: atomic\nm/a.go:1.1,2.1 2 1\nm/a.go:3.1,4.1 1 0\n")
	subprocess := parseProfile(t, "mode: atomic\nm/a.go:3.1,4.1 1 4\nm/b.go:1.1,2.1 1 0\n")

	merged, err := repopolicy.MergeProfiles(unit, subprocess)
	if err != nil {
		t.Fatal(err)
	}

	var out strings.Builder

	err = merged.Write(&out)
	if err != nil {
		t.Fatal(err)
	}

	want := "mode: atomic\nm/a.go:1.1,2.1 2 1\nm/a.go:3.1,4.1 1 4\nm/b.go:1.1,2.1 1 0\n"
	if out.String() != want {
		t.Fatalf("merged profile:\n%s\nwant:\n%s", out.String(), want)
	}

	_, err = repopolicy.MergeProfiles(unit, parseProfile(t, "mode: set\nm/a.go:1.1,2.1 2 1\n"))
	if err == nil {
		t.Fatal("merged profiles with different modes")
	}
}

func TestEvaluateCoverageMethodReceiver(t *testing.T) {
	t.Parallel()

	profile := parseProfile(t, "mode: set\nexample.test/m/pkg/file.go:5.30,6.10 1 1\nexample.test/m/pkg/file.go:6.10,8.3 1 0\n")

	result := repopolicy.EvaluateCoverage(repopolicy.CoverageInput{
		Profile:   profile,
		Entries:   sampleRegistry(t, runnerEntry).Exceptions,
		Module:    coverageModule,
		GOOS:      testLinuxOS,
		Threshold: 90,
		Read:      sourceFrom(map[string]string{coveragePath: runnerSource}),
	})
	if len(result.Problems) != 0 || result.Excluded != 1 {
		t.Fatalf("problems %q excluded %d", result.Problems, result.Excluded)
	}
}
