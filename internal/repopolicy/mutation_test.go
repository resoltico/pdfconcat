// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	conditionalBoundaryOperator = "CONDITIONALS_BOUNDARY"
	uncoveredStatus             = "NOT COVERED"
	killedStatus                = "KILLED"
	mutationFile                = "pkg/total.go"
	mutationSource              = "package pkg\n\nfunc Total(a, b int) int {\n\tif a >= b {\n\t\treturn a - b\n\t}\n\n\treturn b - a\n}\n"

	mutantBoundary = `{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":7}`
	mutantArith    = `{"type":"ARITHMETIC_BASE","status":"LIVED","line":5,"column":10}`

	acceptancesRegistry = `
  - id: mutation-total-boundary
    tool: mutation
    kind: equivalent-mutant
    path: pkg/total.go
    operator: CONDITIONALS_BOUNDARY
    anchor: if a >= b {
    column: 7
    statuses: [lived]
    retained_property: equal operands give equal results either way
  - id: mutation-wrong-status
    tool: mutation
    kind: equivalent-mutant
    path: pkg/total.go
    operator: ARITHMETIC_BASE
    anchor: return a - b
    column: 12
    statuses: [timed-out]
    retained_property: the result is only observed through the boundary
  - id: mutation-gone
    tool: mutation
    kind: equivalent-mutant
    path: pkg/total.go
    operator: INVERT_NEGATIVES
    anchor: return b - a
    column: 11
    statuses: [lived]
    retained_property: negation never occurs here
`
)

// mutationReport builds a gremlins report with the given mutations in pkg/total.go and one in a
// Windows-only file, which a non-Windows run does not judge.
func mutationReport(t *testing.T, mutations string) *repopolicy.MutationReport {
	t.Helper()

	other := `{"type":"CONDITIONALS_NEGATION","status":"NOT COVERED","line":4,"column":5}`

	report, err := repopolicy.ParseMutationReport([]byte(
		`{"files":[{"file_name":"pkg/total.go","mutations":[` + mutations + `]},` +
			`{"file_name":"pkg/total_windows.go","mutations":[` + other + `]}]}`))
	if err != nil {
		t.Fatal(err)
	}

	return report
}

func mutationHost() map[string]bool {
	return map[string]bool{mutationFile: true}
}

func totalSource() repopolicy.SourceReader {
	return sourceFrom(map[string]string{mutationFile: mutationSource, "pkg/total_windows.go": mutationSource})
}

func TestEvaluateMutationPassesWhenEverythingIsKilled(t *testing.T) {
	t.Parallel()

	report := mutationReport(t, `{"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":4,"column":7},`+
		`{"type":"ARITHMETIC_BASE","status":"NOT VIABLE","line":5,"column":10}`)

	result := repopolicy.EvaluateMutation(report, nil, mutationHost(), totalSource())
	if len(result.Problems) != 0 || result.Evaluated != 2 || result.OtherPlatform != 1 {
		t.Fatalf("%+v", result)
	}

	if !strings.Contains(strings.Join(result.SummaryLines(), "\n"), "KILLED       1") {
		t.Fatalf("summary lacks the denominators: %q", result.SummaryLines())
	}
}

// TestEvaluateMutationFailsEveryUnkilledStatus is the negative control: survivors, timeouts and
// uncovered mutants each fail the gate; only judged outcomes can be excepted.
func TestEvaluateMutationFailsEveryUnkilledStatus(t *testing.T) {
	t.Parallel()

	report := mutationReport(t, mutantBoundary+`,`+
		`{"type":"ARITHMETIC_BASE","status":"TIMED OUT","line":5,"column":10},`+
		`{"type":"ARITHMETIC_BASE","status":"NOT COVERED","line":8,"column":10},`+
		`{"type":"INVERT_NEGATIVES","status":"BOGUS","line":8,"column":10}`)

	result := repopolicy.EvaluateMutation(report, nil, mutationHost(), totalSource())

	requireContains(t, result.Problems, "pkg/total.go:4:7: CONDITIONALS_BOUNDARY mutant lived not killed")
	requireContains(t, result.Problems, "pkg/total.go:5:10: ARITHMETIC_BASE mutant timed out not killed")
	requireContains(t, result.Problems, "pkg/total.go:8:10: ARITHMETIC_BASE mutant not covered not killed")
	requireContains(t, result.Problems, `unexpected mutant status "BOGUS"`)
}

func TestEvaluateMutationAcceptsOnlyPreciseEntries(t *testing.T) {
	t.Parallel()

	entries := sampleRegistry(t, acceptancesRegistry).Exceptions
	result := repopolicy.EvaluateMutation(mutationReport(t, mutantBoundary+","+mutantArith), entries, mutationHost(), totalSource())

	if result.Accepted != 1 {
		t.Fatalf("accepted %d, want only the exact boundary entry", result.Accepted)
	}

	requireContains(t, result.Problems, "pkg/total.go:5:10: ARITHMETIC_BASE mutant lived not killed")
	requireContains(t, result.Problems, "mutation-wrong-status: accepts no surviving mutant")
	requireContains(t, result.Problems, "mutation-gone: accepts no surviving mutant")

	const wantProblems = 3
	if len(result.Problems) != wantProblems {
		t.Fatalf("problems %q", result.Problems)
	}
}

func TestEvaluateMutationEmptyReportFails(t *testing.T) {
	t.Parallel()

	result := repopolicy.EvaluateMutation(&repopolicy.MutationReport{}, nil, mutationHost(), sourceFrom(nil))
	requireContains(t, result.Problems, "nothing was measured")
}

func TestParseMutationReportRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := repopolicy.ParseMutationReport([]byte("panic: something"))
	if err == nil {
		t.Fatal("garbage accepted as a report")
	}
}

func TestMutationOperatorsMatchTheToolFlags(t *testing.T) {
	t.Parallel()

	const operators = 11
	if len(repopolicy.MutationOperators()) != operators {
		t.Fatalf("gremlins v0.6.0 has eleven operators; got %v", repopolicy.MutationOperators())
	}
}

// TestMutationDiscoveryRejectsIncompleteEvidence applies real missing-file/operator controls.
func TestMutationDiscoveryRejectsIncompleteEvidence(t *testing.T) {
	t.Parallel()

	first := repopolicy.Mutant{
		File: mutationFile, Type: conditionalBoundaryOperator, Status: discoveredStatus,
		DiscoveryStatus: discoveredStatus, Line: 4, Column: 7,
	}
	second := repopolicy.Mutant{
		File: mutationFile, Type: "ARITHMETIC_BASE", Status: discoveredStatus,
		DiscoveryStatus: discoveredStatus, Line: 5, Column: 10,
	}
	discovery := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{first, second}}
	first.Status, second.Status = killedStatus, "NOT VIABLE"
	first.ExecutionReference, second.ExecutionReference = strings.Repeat("a", 64), strings.Repeat("b", 64)

	complete := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{first, second}}
	if problems := repopolicy.MutationDiscoveryIssues(discovery, complete, mutationHost()); len(problems) != 0 {
		t.Fatal(problems)
	}

	controls := map[string]*repopolicy.MutationReport{
		"truncated": {Mutants: []repopolicy.Mutant{first}},
		"empty":     {},
		"duplicate": {Mutants: []repopolicy.Mutant{first, first}},
	}
	for name, control := range controls {
		if problems := repopolicy.MutationDiscoveryIssues(discovery, control, mutationHost()); len(problems) == 0 {
			t.Errorf("%s accepted", name)
		}
	}

	complete.Mutants[1].File = "unknown.go"
	if problems := repopolicy.MutationDiscoveryIssues(discovery, complete, mutationHost()); len(problems) == 0 {
		t.Fatal("unknown path accepted")
	}

	complete.Mutants[1] = second

	complete.Mutants[1].Status = discoveredStatus
	if problems := repopolicy.MutationDiscoveryIssues(discovery, complete, mutationHost()); len(problems) == 0 {
		t.Fatal("unfinished campaign accepted")
	}

	host := mutationHost()
	host["pkg/missing.go"] = true
	result := repopolicy.EvaluateMutation(&repopolicy.MutationReport{Mutants: []repopolicy.Mutant{first}}, nil, host, totalSource())
	requireContains(t, result.Problems, "report missing host file")
}

func TestUnjudgedMutationCannotBeAcceptedByRegistry(t *testing.T) {
	t.Parallel()

	entries := sampleRegistry(t, `
  - id: mutation-unjudged-control
    tool: mutation
    kind: equivalent-mutant
    path: pkg/total.go
    operator: CONDITIONALS_BOUNDARY
    anchor: if a >= b {
    column: 7
    statuses: [lived]
    retained_property: this control must not convert an unjudged result into completion
`).Exceptions

	// Bypass grammar to prove even a would-be matching entry cannot hide an unexecuted result.
	entries[0].Statuses = []string{"not-covered"}

	report := mutationReport(t, `{"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED","line":4,"column":7}`)

	result := repopolicy.EvaluateMutation(report, entries, mutationHost(), totalSource())
	if result.Accepted != 0 || len(result.Problems) == 0 {
		t.Fatalf("matching registry entry hid unjudged mutant: %+v", result)
	}
}

func TestMutationDiscoveryPreservesOriginalCoverageFacts(t *testing.T) {
	t.Parallel()

	original := repopolicy.Mutant{
		File: mutationFile, Type: conditionalBoundaryOperator, Status: uncoveredStatus,
		DiscoveryStatus: uncoveredStatus, Line: 4, Column: 7,
	}
	discovery := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{original}}
	judged := original
	judged.Status = killedStatus
	judged.ExecutionReference = strings.Repeat("a", 64)

	campaign := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{judged}}
	if problems := repopolicy.MutationDiscoveryIssues(discovery, campaign, mutationHost()); len(problems) != 0 {
		t.Fatalf("actually judged uncovered token rejected: %v", problems)
	}

	for _, altered := range []string{"", discoveredStatus, "SKIPPED"} {
		campaign.Mutants[0].DiscoveryStatus = altered
		if problems := repopolicy.MutationDiscoveryIssues(discovery, campaign, mutationHost()); len(problems) == 0 {
			t.Fatalf("original coverage fact changed to %q without rejection", altered)
		}
	}

	campaign.Mutants[0] = original
	if problems := repopolicy.MutationDiscoveryIssues(discovery, campaign, mutationHost()); len(problems) == 0 {
		t.Fatal("final unjudged status accepted")
	}

	campaign.Mutants[0] = judged

	discovery.Mutants[0].DiscoveryStatus = discoveredStatus
	if problems := repopolicy.MutationDiscoveryIssues(discovery, campaign, mutationHost()); len(problems) == 0 {
		t.Fatal("discovery final/original coverage mismatch accepted")
	}
}

func TestParseMutationReportRetainsExecutionAndDiscoveryReferences(t *testing.T) {
	t.Parallel()

	reference := strings.Repeat("b", 64)
	data := `{"files":[{"file_name":"pkg/total.go","mutations":[{"type":"CONDITIONALS_BOUNDARY",` +
		`"status":"KILLED","discovery_status":"NOT COVERED","execution_reference":"` + reference + `","line":4,"column":7}]}]}`

	report, err := repopolicy.ParseMutationReport([]byte(data))
	if err != nil || len(report.Mutants) != 1 {
		t.Fatalf("parse corrected tool report: %v %+v", err, report)
	}

	if report.Mutants[0].DiscoveryStatus != uncoveredStatus || report.Mutants[0].ExecutionReference != reference {
		t.Fatalf("original/execution telemetry lost: %+v", report.Mutants[0])
	}
}

func TestMutationDiscoveryRequiresUniqueValidExecutionReferences(t *testing.T) {
	t.Parallel()

	first := repopolicy.Mutant{
		File: mutationFile, Type: conditionalBoundaryOperator, Status: discoveredStatus,
		DiscoveryStatus: discoveredStatus, Line: 4, Column: 7,
	}
	second := first
	second.Column++
	discovery := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{first, second}}
	first.Status, second.Status = killedStatus, "KILLED"
	first.ExecutionReference, second.ExecutionReference = strings.Repeat("a", 64), strings.Repeat("b", 64)

	campaign := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{first, second}}
	for _, invalid := range []string{"", "../outside", strings.Repeat("B", 64), strings.Repeat("g", 64), first.ExecutionReference} {
		campaign.Mutants[1].ExecutionReference = invalid
		if problems := repopolicy.MutationDiscoveryIssues(discovery, campaign, mutationHost()); len(problems) == 0 {
			t.Fatalf("invalid/duplicate execution reference accepted: %q", invalid)
		}
	}
}

func TestMutationExceptionDoesNotAcceptSecondOperatorOnSameLine(t *testing.T) {
	t.Parallel()

	const source = "package pkg\nfunc Ordered(a, b, c int) bool {\n\tif a < b && b < c {\n\t\treturn true\n\t}\n\treturn false\n}\n"

	entries := sampleRegistry(t, `
  - id: mutation-first-boundary-only
    tool: mutation
    kind: equivalent-mutant
    path: pkg/total.go
    operator: CONDITIONALS_BOUNDARY
    anchor: if a < b && b < c {
    column: 7
    statuses: [lived]
    retained_property: supported operands a and b differ, so only the first boundary is equivalent
`).Exceptions
	first := repopolicy.Mutant{File: mutationFile, Type: conditionalBoundaryOperator, Status: "LIVED", Line: 3, Column: 7}
	second := first
	second.Column = 16

	report := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{first, second}}

	result := repopolicy.EvaluateMutation(report, entries, mutationHost(), sourceFrom(map[string]string{mutationFile: source}))
	if result.Accepted != 1 || len(result.Problems) != 1 {
		t.Fatalf("first operator's exception accepted the second boundary: %+v", result)
	}
}

func TestMutationCompletenessCannotBeInferredFromKilledRows(t *testing.T) {
	t.Parallel()

	row := `{"type":"CONDITIONALS_BOUNDARY","status":"RUNNABLE","discovery_status":"RUNNABLE","line":4,"column":7}`

	discovery, err := repopolicy.ParseMutationReport(
		[]byte(`{"complete":true,"files":[{"file_name":"pkg/total.go","mutations":[` + row + `]}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	killed := strings.Replace(row, `"status":"RUNNABLE"`, `"status":"KILLED"`, 1)

	killed = strings.TrimSuffix(killed, "}") + `,"execution_reference":"` + strings.Repeat("a", 64) + `"}`
	for _, completion := range []string{"", `"complete":false,`, `"complete":null,`} {
		data := `{` + completion + `"termination_reason":"deadline","files":[{"file_name":"pkg/total.go","mutations":[` + killed + `]}]}`

		campaign, parseErr := repopolicy.ParseMutationReport([]byte(data))
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		problems := repopolicy.MutationDiscoveryIssues(discovery, campaign, mutationHost())
		requireContains(t, problems, "campaign lacks complete:true: deadline")
	}

	completed := &repopolicy.MutationReport{
		Complete:          true,
		TerminationReason: "deadline",
		Mutants:           append([]repopolicy.Mutant(nil), discovery.Mutants...),
	}
	completed.Mutants[0].Status = killedStatus
	completed.Mutants[0].ExecutionReference = strings.Repeat("a", 64)
	requireContains(
		t,
		repopolicy.MutationDiscoveryIssues(discovery, completed, mutationHost()),
		"completed mutation campaign carries a termination reason",
	)
	discovery.TerminationReason = "deadline"
	completed.TerminationReason = ""
	requireContains(
		t,
		repopolicy.MutationDiscoveryIssues(discovery, completed, mutationHost()),
		"completed mutation discovery carries a termination reason",
	)
	discovery.TerminationReason = ""

	discovery.Complete = false
	campaign := *discovery
	campaign.Complete = true

	campaign.Mutants = append([]repopolicy.Mutant(nil), discovery.Mutants...)
	campaign.Mutants[0].Status = killedStatus
	campaign.Mutants[0].ExecutionReference = strings.Repeat("a", 64)
	requireContains(t, repopolicy.MutationDiscoveryIssues(discovery, &campaign, mutationHost()), "discovery lacks complete:true")
}

func TestMutationDiscoveryPreflightRetainsUncoveredHostAndRejectsForeign(t *testing.T) {
	t.Parallel()

	host := repopolicy.Mutant{
		File:            mutationFile,
		Type:            "CONDITIONALS_NEGATION",
		Status:          "NOT COVERED",
		DiscoveryStatus: "NOT COVERED",
		Line:            97,
		Column:          14,
	}

	report := &repopolicy.MutationReport{Complete: true, Mutants: []repopolicy.Mutant{host}}
	if problems := repopolicy.MutationDiscoveryPreflightIssues(report, mutationHost()); len(problems) != 0 {
		t.Fatal(problems)
	}

	foreign := host
	foreign.File = "third_party/pdfcpu/pdfcpu/pkg/pdfcpu/zoom.go"

	report.Mutants = append(report.Mutants, foreign)
	if problems := repopolicy.MutationDiscoveryPreflightIssues(report, mutationHost()); len(problems) == 0 {
		t.Fatal("foreign discovery reached execution")
	}

	report.Mutants = []repopolicy.Mutant{host}

	report.Complete = false
	if problems := repopolicy.MutationDiscoveryPreflightIssues(report, mutationHost()); len(problems) == 0 {
		t.Fatal("partial discovery reached execution")
	}
}
