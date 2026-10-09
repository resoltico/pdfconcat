// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// This diagnostic was captured from the installed source-contracts linter's real gosec run.
// Only Filename is normalized; the complete receipt remains in the execution audit.
const (
	lintReportRoot             = "/work/repo"
	multilinePosition          = `"Line":29`
	multilineProcessDiagnostic = `{"Issues":[{
"FromLinter":"gosec",
"Text":"G204: Subprocess launched with a potential tainted input or cmd arguments",
"SourceLines":[
"\t\t\tcommand := exec.CommandContext(",
"\t\t\t\tt.Context(),",
"\t\t\t\texecutable,",
"\t\t\t\tprogressHelperArgs(\"-test.run=^TestProgressClosedStandardDescriptorHelper$\")...)"],
"Pos":{"Filename":"/work/repo/cmd/pdfconcat/progress_transport_standard_unix_test.go","Line":29,"Column":15},
"LineRange":{"From":29,"To":32}}]}`
)

func TestStalenessMatchesPhysicalLineOfRealMultilineDiagnostic(t *testing.T) {
	t.Parallel()

	issues, err := repopolicy.ParseIssues([]byte(multilineProcessDiagnostic), lintReportRoot)
	if err != nil {
		t.Fatal(err)
	}

	entry := &repopolicy.Entry{
		Tool: repopolicy.ToolLint, Effect: repopolicy.EffectExcludeDiagnostic,
		Linter: policySecurityLinter, Path: "cmd/pdfconcat/progress_transport_standard_unix_test.go",
		Message: "G204: Subprocess launched with a potential tainted input or cmd arguments",
		Source:  `progressHelperArgs\(`,
	}
	if len(repopolicy.StaleDiagnosticEntries([]*repopolicy.Entry{entry}, issues, testLinuxOS)) != 1 {
		t.Fatal("a different displayed source line counted as a matching physical exclusion")
	}

	entry.Source = `command := exec\.CommandContext\(`
	if len(repopolicy.StaleDiagnosticEntries([]*repopolicy.Entry{entry}, issues, testLinuxOS)) != 0 {
		t.Fatal("the actual diagnostic position did not match its physical exclusion")
	}
}

func TestDiagnosticSourceRangeSelectsPositionAndRejectsIncompleteEvidence(t *testing.T) {
	t.Parallel()

	interior := strings.Replace(multilineProcessDiagnostic, multilinePosition, `"Line":30`, 1)
	issues, err := repopolicy.ParseIssues([]byte(interior), lintReportRoot)

	if err != nil || len(issues) != 1 || strings.TrimSpace(issues[0].Source) != "t.Context()," {
		t.Fatalf("interior position did not select its source line: %+v, %v", issues, err)
	}

	for _, change := range []struct{ old, replacement string }{
		{multilinePosition, `"Line":28`},
		{multilinePosition, `"Line":33`},
		{`"To":32`, `"To":28`},
		{`"To":32`, `"To":33`},
	} {
		invalid := strings.Replace(multilineProcessDiagnostic, change.old, change.replacement, 1)
		if _, err = repopolicy.ParseIssues([]byte(invalid), lintReportRoot); err == nil {
			t.Fatalf("inconsistent source range accepted: %s", invalid)
		}
	}
}

func TestDiagnosticSourceRejectsAmbiguousFallbackRanges(t *testing.T) {
	t.Parallel()

	for _, change := range []struct{ old, replacement string }{
		{",\n" + `"LineRange":{"From":29,"To":32}`, ""},
		{`"From":29`, `"From":0`},
		{`"From":29,"To":32`, `"From":0,"To":0`},
		{`"From":29`, `"From":-1`},
		{multilinePosition, `"Line":0`},
		{multilinePosition, `"Line":-1`},
	} {
		invalid := strings.Replace(multilineProcessDiagnostic, change.old, change.replacement, 1)
		if invalid == multilineProcessDiagnostic {
			t.Fatalf("negative control did not change the report: %s", change.old)
		}

		if _, err := repopolicy.ParseIssues([]byte(invalid), lintReportRoot); err == nil {
			t.Fatalf("ambiguous diagnostic source accepted: %s", invalid)
		}
	}
}

func TestDiagnosticSourceSingletonFallbackAndAbsentSource(t *testing.T) {
	t.Parallel()

	const singleton = `{"Issues":[{"FromLinter":"gosec","Text":"G204",
"SourceLines":["command := exec.CommandContext("],"Pos":{"Filename":"a.go","Line":29}}]}`

	for _, raw := range []string{
		singleton,
		strings.Replace(singleton, `"Line":29}`, `"Line":29},"LineRange":{"From":0,"To":0}`, 1),
	} {
		issues, err := repopolicy.ParseIssues([]byte(raw), lintReportRoot)
		if err != nil || len(issues) != 1 || issues[0].Source != "command := exec.CommandContext(" {
			t.Fatalf("physical singleton was not preserved: %+v, %v", issues, err)
		}
	}

	const absent = `{"Issues":[{"FromLinter":"gosec","Text":"G204","Pos":{"Filename":"a.go"}}]}`

	issues, err := repopolicy.ParseIssues([]byte(absent), lintReportRoot)
	if err != nil || len(issues) != 1 || issues[0].Source != "" {
		t.Fatalf("absent source must remain absent: %+v, %v", issues, err)
	}
}
