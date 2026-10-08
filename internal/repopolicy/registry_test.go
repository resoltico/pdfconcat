// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	disableWSLID   = "lint-disable-wsl"
	kindDeprecated = "deprecated-rule"
	wantExactFile  = "exact file"
	wantGoSource   = "Go source file"
	funcRun        = "Run"

	// disableEntry is a valid lint entry; tests derive invalid registries from it.
	disableEntry = rationaleComment + `  - id: lint-disable-wsl
    tool: golangci-lint
    kind: deprecated-rule
    effect: disable-linter
    linter: wsl
    retained_property: whitespace rules stay enforced
`

	diagnosticEntry = rationaleComment + `  - id: lint-example
    tool: golangci-lint
    kind: intended-pattern
    effect: exclude-diagnostic
    linter: gosec
    path: internal/example/file.go
    message: "` + fileReadDiagnostic + `"
    retained_property: paths are validated first
`

	coverageEntry = rationaleComment + `  - id: coverage-example
    tool: coverage
    kind: unreachable-branch
    path: internal/example/file.go
    function: Run
    anchor: if err != nil {
    retained_property: errors are returned
`

	mutationEntry = rationaleComment + `  - id: mutation-example
    tool: mutation
    kind: equivalent-mutant
    path: internal/example/file.go
    operator: CONDITIONALS_BOUNDARY
    anchor: if count > 0 {
    column: 10
    statuses: [lived]
    retained_property: both sides behave identically
`

	allToolsRegistry = registryHeader + disableEntry + rationaleComment + `  - id: lint-example
    tool: golangci-lint
    kind: intended-pattern
    effect: exclude-diagnostic
    linter: gosec
    path: internal/example/file.go
    message: "` + fileReadDiagnostic + `"
    source: os\.ReadFile\(name\)
    goos: linux
    retained_property: paths are validated first
` + rationaleComment + `  - id: coverage-example
    tool: coverage
    kind: unreachable-platform-branch
    goos: windows
    path: internal/example/file.go
    function: Runner.Run
    anchor: if err != nil {
    retained_property: errors are returned
` + rationaleComment + `  - id: mutation-example
    tool: mutation
    kind: equivalent-mutant
    path: internal/example/file.go
    operator: CONDITIONALS_BOUNDARY
    anchor: if count > 0 {
    column: 10
    statuses: [lived, timed-out]
    retained_property: both sides behave identically
`
)

func TestParseRegistryAcceptsValidEntry(t *testing.T) {
	t.Parallel()

	registry, err := repopolicy.ParseRegistry([]byte(registryHeader + disableEntry))
	if err != nil {
		t.Fatal(err)
	}

	if len(registry.Exceptions) != 1 || registry.Exceptions[0].ID != disableWSLID {
		t.Fatalf("unexpected registry %+v", registry)
	}

	if !strings.Contains(registry.Exceptions[0].Rationale, "repair would contradict") {
		t.Fatalf("rationale not attached: %q", registry.Exceptions[0].Rationale)
	}
}

// TestParseRegistryRejectsInvalidEntry is the negative control for the registry validator: every way
// an entry can be missing, un-defended or broader than it should be is rejected with a specific reason.
func TestParseRegistryRejectsInvalidEntry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		entry       string
		old         string
		replacement string
		want        string
	}{
		{"missing comment", disableEntry, rationaleComment, "", "comment directly above"},
		{"short comment", disableEntry, rationaleComment, "  # because\n", "comment directly above"},
		{"unknown tool", disableEntry, "tool: golangci-lint", "tool: sonar", `tool "sonar"`},
		{"bad id", disableEntry, "lint-disable-wsl", "Disable_WSL", "id must be"},
		{"wrong kind for tool", disableEntry, kindDeprecated, "equivalent-mutant", "not valid for golangci-lint"},
		{"missing property", disableEntry, "whitespace rules stay enforced", "ok", "retained_property"},
		{"unknown field", disableEntry, "linter: wsl\n", "linter: wsl\n    surprise: 1\n", "field surprise not found"},
		{"lint without effect", disableEntry, "    effect: disable-linter\n", "", "effect"},
		{"incompatible without conflict", disableEntry, kindDeprecated, "incompatible-rule", "conflicts_with"},
		{"duplicate without superseder", disableEntry, kindDeprecated, "duplicate-rule", "superseded_by"},
		{"setting item without values", disableEntry, "effect: disable-linter", "effect: setting-item", "values is required"},
		{"diagnostic with glob path", diagnosticEntry, examplePath, "internal/example/*.go", wantExactFile},
		{"diagnostic with test-wide glob", diagnosticEntry, examplePath, `"**/*_test.go"`, wantExactFile},
		{"diagnostic with directory", diagnosticEntry, examplePath, "internal/example", wantGoSource},
		{"diagnostic with parent path", diagnosticEntry, examplePath, "../x.go", "relative"},
		{"diagnostic with regex path", diagnosticEntry, examplePath, "internal/(a|b).go", wantExactFile},
		{"diagnostic without message", diagnosticEntry, "    message: \"G304: Potential file inclusion\"\n", "", "message is required"},
		{"diagnostic with catch-all message", diagnosticEntry, fileReadDiagnostic, ".*", broadPatternProblem},
		{"diagnostic with bad regex", diagnosticEntry, fileReadDiagnostic, "(unclosed message", "valid regular expression"},
		{"coverage without anchor", coverageEntry, "    anchor: if err != nil {\n", "", "anchor is required"},
		{"coverage platform kind without goos", coverageEntry, "unreachable-branch", "unreachable-platform-branch", "goos"},
		{"coverage with directory path", coverageEntry, examplePath, "internal/example/", wantGoSource},
		{"mutation with package path", mutationEntry, examplePath, "internal/example", wantGoSource},
		{"mutation with unknown operator", mutationEntry, conditionalBoundaryOperator, "ALL", "operator"},
		{"mutation without anchor", mutationEntry, "    anchor: if count > 0 {\n", "", "anchor is required"},
		{"mutation without statuses", mutationEntry, "    statuses: [lived]\n", "", "statuses is required"},
		{
			"mutation field of another tool",
			mutationEntry,
			"operator: CONDITIONALS_BOUNDARY",
			"operator: CONDITIONALS_BOUNDARY\n    linter: gosec",
			"linter does not apply",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			content := registryHeader + mustReplace(t, test.entry, test.old, test.replacement)

			_, err := repopolicy.ParseRegistry([]byte(content))
			if err == nil {
				t.Fatal("registry accepted")
			}

			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %q does not mention %q", err, test.want)
			}
		})
	}
}

func TestParseRegistryRejectsInvalidDocument(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"duplicate id", registryHeader + disableEntry + disableEntry, "duplicate id"},
		{
			"duplicate coverage scope",
			registryHeader + coverageEntry + strings.Replace(coverageEntry, "coverage-example", "coverage-same-statement", 1),
			"duplicate coverage scope",
		},
		{"threshold out of range", strings.Replace(registryHeader, "100", "120", 1) + disableEntry, coverageThresholdField},
		{"lowered threshold", strings.Replace(registryHeader, "100", "99.9", 1) + disableEntry, coverageThresholdField},
		{"null exception", registryHeader + "  - null\n", "null exception"},
		{"zero threshold", strings.Replace(registryHeader, "100", "0", 1) + disableEntry, coverageThresholdField},
		{"wrong version", strings.Replace(registryHeader, "version: 1", "version: 2", 1) + disableEntry, "version is 2"},
		{"trailing document", registryHeader + disableEntry + "\n---\nunknown: true\n", "one YAML document"},
		{"trailing empty document", registryHeader + disableEntry + "\n---\n", "one YAML document"},
		{"not yaml", "exceptions: [unclosed", "decode"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := repopolicy.ParseRegistry([]byte(test.content))
			if !errors.Is(err, repopolicy.ErrRegistry) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want an ErrRegistry mentioning %q", err, test.want)
			}
		})
	}
}

func TestParseRegistryAcceptsPreciseEntriesOfEveryTool(t *testing.T) {
	t.Parallel()

	registry, err := repopolicy.ParseRegistry([]byte(allToolsRegistry))
	if err != nil {
		t.Fatal(err)
	}

	lint := registry.For(repopolicy.ToolLint)
	if len(lint) != 2 || len(registry.For(repopolicy.ToolCoverage)) != 1 || len(registry.For(repopolicy.ToolMutation)) != 1 {
		t.Fatalf("entries not grouped by tool: %+v", registry.Exceptions)
	}
}

func TestLoadRegistryReportsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := repopolicy.LoadRegistry(filepath.Join(t.TempDir(), "absent.yml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v, want a not-exist error", err)
	}
}

func TestRepositoryIssuesFlagsVanishedCode(t *testing.T) {
	t.Parallel()

	const source = "package pkg\n\nfunc Run() error {\n\tif err := step(); err != nil {\n\t\treturn err\n\t}\n\n\treturn nil\n}\n"

	read := sourceFrom(map[string]string{coveragePath: source})
	anchor := "if err := step(); err != nil {"
	mutationColumn := 12

	entries := []*repopolicy.Entry{
		{ID: "coverage-ok", Tool: repopolicy.ToolCoverage, Path: coveragePath, Function: funcRun, Anchor: anchor},
		{ID: "coverage-renamed", Tool: repopolicy.ToolCoverage, Path: coveragePath, Function: "Execute", Anchor: anchor},
		{ID: "coverage-edited", Tool: repopolicy.ToolCoverage, Path: coveragePath, Function: funcRun, Anchor: "if err != nil {"},
		{ID: "coverage-moved", Tool: repopolicy.ToolCoverage, Path: "pkg/other.go", Function: funcRun, Anchor: "x"},
		{
			ID:       "mutation-edited",
			Tool:     repopolicy.ToolMutation,
			Path:     coveragePath,
			Operator: "ARITHMETIC_BASE",
			Anchor:   "total := a + b",
			Column:   &mutationColumn,
		},
	}

	problems := repopolicy.RepositoryIssues(entries, read)

	requireContains(t, problems, "coverage-renamed: coverage profile: pkg/file.go has no function Execute")
	requireContains(t, problems, "coverage-edited: no line of Run")
	requireContains(t, problems, "coverage-moved: cannot read pkg/other.go")
	requireContains(t, problems, "mutation-edited: mutation report: mutation anchor is missing")

	const wantProblems = 4
	if len(problems) != wantProblems {
		t.Fatalf("got %d problems, want %d (coverage-ok is current): %q", len(problems), wantProblems, problems)
	}
}

// TestRegistryRejectsBroadPredicates requires each diagnostic matcher to identify literal text.
func TestRegistryRejectsBroadPredicates(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"foobar|.*", "(foobar)?", "specific.*diagnostic", "[A-Z]+"} {
		content := registryHeader + mustReplace(t, diagnosticEntry, fileReadDiagnostic, pattern)

		_, err := repopolicy.ParseRegistry([]byte(content))
		if err == nil || !strings.Contains(err.Error(), broadPatternProblem) {
			t.Errorf("%q: %v", pattern, err)
		}
	}
}

func TestRegistryCannotAcceptUnjudgedMutationStatus(t *testing.T) {
	t.Parallel()

	content := registryHeader + mustReplace(t, mutationEntry, "statuses: [lived]", "statuses: [not-covered]")

	_, err := repopolicy.ParseRegistry([]byte(content))
	if !errors.Is(err, repopolicy.ErrRegistry) || !strings.Contains(err.Error(), `status "not-covered" is not one`) {
		t.Fatalf("unjudged registry acceptance allowed: %v", err)
	}
}

func TestDesignIncompatibleSettingIsRestrictedToReplacedSourceAlgorithms(t *testing.T) {
	t.Parallel()

	for _, rule := range []string{"file-length-limit", "max-public-structs", "function-length", "add-constant"} {
		t.Run(rule, func(t *testing.T) {
			t.Parallel()

			entry := rationaleComment + "  - id: lint-source-algorithm\n" +
				"    tool: golangci-lint\n    kind: design-incompatible\n    effect: setting-item\n" +
				"    setting: linters.settings.revive.rules\n    values: [" + rule + "]\n" +
				"    retained_property: mandatory source scanner enforces limits\n"
			_, err := repopolicy.ParseRegistry([]byte(registryHeader + entry))

			allowed := rule == "file-length-limit" || rule == "max-public-structs"
			if allowed != (err == nil) {
				t.Fatalf("rule %s: allowed=%v, parse error=%v", rule, allowed, err)
			}
		})
	}
}

func TestRegistryRejectsDuplicateLintScopeWithDifferentIDs(t *testing.T) {
	t.Parallel()

	duplicate := strings.Replace(disableEntry, disableWSLID, "lint-other-wsl", 1)

	_, err := repopolicy.ParseRegistry([]byte(registryHeader + disableEntry + duplicate))
	if err == nil || !strings.Contains(err.Error(), "duplicate lint scope") {
		t.Fatalf("duplicated authority accepted: %v", err)
	}
}
