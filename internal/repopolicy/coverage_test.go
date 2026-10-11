// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func parseProfile(t *testing.T, text string) *repopolicy.CoverageProfile {
	t.Helper()

	profile, err := repopolicy.ParseCoverageProfile(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}

	return profile
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

func TestRawCoverageCountsUnexecutedStatementsWithoutExclusion(t *testing.T) {
	profile := parseProfile(t, "mode: atomic\nm/a.go:1.1,2.1 9 1\nm/a.go:3.1,4.1 1 0\n")
	result := repopolicy.EvaluateCoverage(repopolicy.CoverageInput{Profile: profile, Module: "m"})
	if len(result.Problems) != 0 || result.Raw.Total != 10 || result.Raw.Covered != 9 || result.Raw.Percent() != 90 || len(result.Packages) != 1 {
		t.Fatalf("raw measurement changed: %+v", result)
	}
	empty := repopolicy.EvaluateCoverage(repopolicy.CoverageInput{Profile: parseProfile(t, "mode: atomic\n")})
	requireContains(t, empty.Problems, "nothing was measured")
}
