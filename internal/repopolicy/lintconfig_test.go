// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type divergenceCase struct {
	name        string
	old         string
	replacement string
	want        string
}

const (
	gosecSeverityFragment = "    gosec:\n      severity: low"
	registryFile          = ".quality-exceptions.yml"
	golangciFile          = ".golangci.yml"

	rulesList = "    presets: []\n    rules:\n"

	// diagnosticRegistry has one exclusion whose configuration form the tests build by hand.
	diagnosticRegistry = `
  - id: lint-example
    tool: golangci-lint
    kind: intended-pattern
    effect: exclude-diagnostic
    linter: gosec
    path: internal/example/file.go
    message: "` + fileReadDiagnostic + `"
    retained_property: paths are validated first
`

	exactRule = "linters:\n  exclusions:\n    rules:\n      - path: ^internal/example/file\\.go$\n" +
		"        linters: [gosec]\n        text: 'G304: Potential file inclusion'\n"

	staleRegistry = `
  - id: lint-live
    tool: golangci-lint
    kind: intended-pattern
    effect: exclude-diagnostic
    linter: gosec
    path: internal/example/file.go
    message: "` + fileReadDiagnostic + `"
    retained_property: paths are validated first
  - id: lint-stale
    tool: golangci-lint
    kind: intended-pattern
    effect: exclude-diagnostic
    linter: gosec
    path: internal/example/file.go
    message: "G204: Subprocess launched"
    retained_property: arguments are validated first
  - id: lint-other-platform
    tool: golangci-lint
    kind: intended-pattern
    effect: exclude-diagnostic
    linter: gosec
    path: internal/example/file_windows.go
    message: "G204: Subprocess launched"
    goos: windows
    retained_property: arguments are validated first
`

	premiseRegistry = `
  - id: lint-disable-old
    tool: golangci-lint
    kind: deprecated-rule
    effect: disable-linter
    linter: oldlint
    retained_property: replaced linter still checks
  - id: lint-disable-former
    tool: golangci-lint
    kind: deprecated-rule
    effect: disable-linter
    linter: formerlint
    retained_property: replaced linter still checks
  - id: lint-disable-clash
    tool: golangci-lint
    kind: incompatible-rule
    effect: disable-linter
    linter: clashlint
    conflicts_with: [oldlint, ghostlint]
    retained_property: the conflicting linter still checks
`

	linterListing = `{"enabled":[{"name":"clashlint","deprecated":false}],` +
		`"disabled":[{"name":"oldlint","deprecated":true},{"name":"formerlint","deprecated":false}]}`
)

func loadRegistry(t *testing.T) *repopolicy.Registry {
	t.Helper()

	registry, err := repopolicy.LoadRegistry(filepath.Join(repoRoot(t), registryFile))
	if err != nil {
		t.Fatal(err)
	}

	return registry
}

// TestGolangciConfigMatchesRegistry requires .golangci.yml and the registry to agree exactly.
func TestGolangciConfigMatchesRegistry(t *testing.T) {
	t.Parallel()

	problems, err := repopolicy.LintConfigIssues(loadRegistry(t).Exceptions, readRepoFile(t, golangciFile))
	if err != nil {
		t.Fatal(err)
	}

	if len(problems) > 0 {
		t.Fatalf(".golangci.yml and .quality-exceptions.yml disagree:\n%s", strings.Join(problems, "\n"))
	}
}

// listDivergenceCases edits the disable, preset, path and rule lists.
func listDivergenceCases() []divergenceCase {
	return []divergenceCase{
		{"extra disabled linter", "    - wsl\n", "    - wsl\n    - noctx\n", "does not list: linters.disable: noctx"},
		{"missing disabled linter", "    - wsl\n", "", "missing from .golangci.yml: linters.disable: wsl"},
		{"presets", "    presets: []", "    presets: [comments]", "linters.exclusions.presets: comments"},
		{"generated exclusion", "    generated: disable", "    generated: strict", "linters.exclusions.generated is strict"},
		{
			"formatter generated exclusion", "  exclusions:\n    generated: disable\n  settings:",
			"  exclusions:\n    generated: lax\n  settings:", "formatters.exclusions.generated is lax",
		},
		{
			"exclusion paths",
			"    presets: []\n",
			"    presets: []\n    paths:\n      - .*_test\\.go\n",
			"linters.exclusions.paths: .*_test\\.go",
		},
		{
			"test-wide rule", rulesList, rulesList + "      - path: _test\\.go\n        linters: [funlen]\n        text: lines\n",
			"linters.exclusions.rules: path=_test\\.go linter=funlen",
		},
		{
			"rule with unsupported field",
			rulesList,
			rulesList + "      - path-except: x\n        linters: [funlen]\n        text: lines\n",
			"uses field path-except",
		},
		{
			"rule over several linters",
			rulesList,
			rulesList + "      - path: ^a\\.go$\n        linters: [funlen, lll]\n        text: lines\n",
			"must name one path, one linter and a text",
		},
		{
			"rule without text", rulesList, rulesList + "      - path: ^a\\.go$\n        linters: [funlen]\n",
			"must name one path, one linter and a text",
		},
	}
}

// settingDivergenceCases edits linter settings and the pinned strict values.
func settingDivergenceCases() []divergenceCase {
	return []divergenceCase{
		{
			"ignore list in settings",
			"    varnamelen:\n      min-name-length: 3",
			"    varnamelen:\n      min-name-length: 3\n      ignore-type-assert-ok: true",
			"linters.settings.varnamelen.ignore-type-assert-ok: true",
		},
		{
			"allow flag in settings",
			"      allow-unused: false",
			"      allow-unused: true",
			"linters.settings.nolintlint.allow-unused: true",
		},
		{
			"disabled check in settings",
			"        - unnamedResult\n",
			"        - unnamedResult\n        - hugeParam\n",
			"disabled-checks: hugeParam",
		},
		{
			"govet analyzer disabled",
			"    govet:\n      enable-all: true",
			"    govet:\n      enable-all: true\n      disable:\n        - fieldalignment",
			"govet.disable: fieldalignment",
		},
		{
			"revive rule disabled",
			"        - name: add-constant\n          disabled: true",
			"        - name: add-constant\n          disabled: true\n        - name: exported\n          disabled: true",
			"revive.rules: exported",
		},
		{"staticcheck check disabled", `checks: ["all"]`, `checks: ["all", "-ST1000"]`, "staticcheck.checks: -ST1000"},
		{"staticcheck without all", `checks: ["all"]`, `checks: ["SA*"]`, `must include "all"`},
		{
			"gosec rules excluded",
			gosecSeverityFragment,
			"    gosec:\n      excludes: [G304]\n      severity: low",
			"gosec.excludes: G304",
		},
		{
			"nested ignore list",
			"        hugeParam:",
			"        rangeValCopy:\n          skipTestFuncs: true\n        hugeParam:",
			"skipTestFuncs",
		},
		{"issue limit", "max-same-issues: 0", "max-same-issues: 3", "issues.max-same-issues"},
		{"tests skipped", "  tests: true", "  tests: false", "run.tests"},
		{"not all linters", "  default: all", "  default: standard", "linters.default"},
		{"type assertions unchecked", "check-type-assertions: true", "check-type-assertions: false", "errcheck.check-type-assertions"},
		{"unused exports exempt", "      exported-fields-are-used: false\n", "", "unused.exported-fields-are-used"},
	}
}

// TestLintConfigRejectsDivergence is the negative control for the configuration check: each edit
// introduces an exception the registry does not list, removes one it lists, widens one, or loosens a
// pinned strict setting, and must be reported.
func TestLintConfigRejectsDivergence(t *testing.T) {
	t.Parallel()

	registry := loadRegistry(t)
	original := string(readRepoFile(t, golangciFile))

	cases := slices.Concat(listDivergenceCases(), settingDivergenceCases())

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			problems, err := repopolicy.LintConfigIssues(registry.Exceptions, []byte(mustReplace(t, original, test.old, test.replacement)))
			if err != nil {
				t.Fatal(err)
			}

			requireContains(t, problems, test.want)
		})
	}
}

func TestLintConfigRejectsBroaderDiagnosticRule(t *testing.T) {
	t.Parallel()

	registry := sampleRegistry(t, diagnosticRegistry)

	problems, err := repopolicy.LintConfigIssues(registry.Exceptions, []byte(exactRule))
	if err != nil {
		t.Fatal(err)
	}

	for _, problem := range problems {
		if strings.Contains(problem, "linters.exclusions.rules") {
			t.Fatalf("exact rule rejected: %s", problem)
		}
	}

	broad := strings.Replace(exactRule, "^internal/example/file\\.go$", "internal/example/", 1)

	problems, err = repopolicy.LintConfigIssues(registry.Exceptions, []byte(broad))
	if err != nil {
		t.Fatal(err)
	}

	requireContains(t, problems, "missing from .golangci.yml: linters.exclusions.rules: path=^internal/example/file\\.go$")
	requireContains(t, problems, "does not list: linters.exclusions.rules: path=internal/example/")
}

func TestPremiseIssues(t *testing.T) {
	t.Parallel()

	known, err := repopolicy.ParseLinterInfo([]byte(linterListing))
	if err != nil {
		t.Fatal(err)
	}

	problems := repopolicy.PremiseIssues(sampleRegistry(t, premiseRegistry).Exceptions, known)

	requireContains(t, problems, `lint-disable-former: linter "formerlint" is no longer deprecated`)
	requireContains(t, problems, `lint-disable-clash: golangci-lint has no linter "ghostlint"`)
	requireContains(t, problems, `lint-disable-clash: linter "oldlint" is disabled`)

	const wantProblems = 3
	if len(problems) != wantProblems {
		t.Fatalf("got %q, want exactly the three premise failures", problems)
	}
}

func TestParseLinterInfoRejectsEmptyAndMalformed(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`{}`, `not json`} {
		_, err := repopolicy.ParseLinterInfo([]byte(input))
		if err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestStaleDiagnosticEntries(t *testing.T) {
	t.Parallel()

	registry := sampleRegistry(t, staleRegistry)

	report := `{"Issues":[{"FromLinter":"gosec","Text":"G304: Potential file inclusion via variable",` +
		`"Pos":{"Filename":"/work/repo/internal/example/file.go"}}]}`

	issues, err := repopolicy.ParseIssues([]byte(report), "/work/repo")
	if err != nil {
		t.Fatal(err)
	}

	stale := repopolicy.StaleDiagnosticEntries(registry.Exceptions, issues, testLinuxOS)
	if len(stale) != 1 || stale[0].ID != "lint-stale" {
		t.Fatalf("got %+v, want only lint-stale", stale)
	}

	stale = repopolicy.StaleDiagnosticEntries(registry.Exceptions, issues, "windows")
	if len(stale) != 2 {
		t.Fatalf("on windows the platform entry is judged too; got %+v", stale)
	}
}

func TestParseIssuesRejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := repopolicy.ParseIssues([]byte("not json"), "/work/repo")
	if err == nil {
		t.Fatal("garbage accepted as a lint report")
	}
}

func TestConfigWithoutRulesKeepsReviveRules(t *testing.T) {
	t.Parallel()

	config := "linters:\n  settings:\n    revive:\n      rules:\n        - name: add-constant\n          disabled: true\n" +
		"  exclusions:\n    rules:\n      - path: ^a\\.go$\n        linters: [gosec]\n        text: G304 hit\n"

	stripped, err := repopolicy.ConfigWithoutRules([]byte(config))
	if err != nil {
		t.Fatal(err)
	}

	text := string(stripped)
	if strings.Contains(text, "G304 hit") || !strings.Contains(text, "add-constant") {
		t.Fatalf("exclusion rule not removed or revive rule lost:\n%s", text)
	}
}

// TestRewrittenConfigKeepsItsOtherExceptions checks the file the staleness run lints with: it must
// lack exactly the exclusion rules and keep every other exception, such as disabled revive rules.
func TestRewrittenConfigKeepsItsOtherExceptions(t *testing.T) {
	t.Parallel()

	stripped, err := repopolicy.ConfigWithoutRules(readRepoFile(t, golangciFile))
	if err != nil {
		t.Fatal(err)
	}

	problems, err := repopolicy.LintConfigIssues(loadRegistry(t).Exceptions, stripped)
	if err != nil {
		t.Fatal(err)
	}

	for _, problem := range problems {
		if !strings.Contains(problem, "missing from .golangci.yml: linters.exclusions.rules") {
			t.Fatalf("rewriting the configuration changed something besides the exclusion rules: %s", problem)
		}
	}

	if len(problems) == 0 {
		t.Fatal("the exclusion rules were not removed")
	}
}

// TestLintFiltersCannotReduceAnalysis exercises the audit's selective-enabling bypasses.
func TestLintFiltersCannotReduceAnalysis(t *testing.T) {
	t.Parallel()

	registry, err := repopolicy.LoadRegistry(filepath.Join(repoRoot(t), ".quality-exceptions.yml"))
	if err != nil {
		t.Fatal(err)
	}

	config, err := os.ReadFile(filepath.Join(repoRoot(t), ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}

	changes := [][2]string{
		{"severity: low", "severity: high"},
		{"confidence: low", "confidence: high"},
		{"new: false", "new: true"},
		{"issues-exit-code: 1", "issues-exit-code: 0"},
		{"disable-default-exclusions: true", "disable-default-exclusions: false"},
		{"check-exported: true", "check-exported: false"},
		{"multi-if: false", "multi-if: true"},
		{"multi-func: false", "multi-func: true"},
		{"new-from-rev: \"\"", "new-from-rev: main"},
		{gosecSeverityFragment, "    gosec:\n      config: {global: {nosec: enabled}}\n      severity: low"},
		{"    godoclint:\n      default: all", "    godoclint:\n      default: none"},
		{gosecSeverityFragment, "    gosec:\n      includes: [G401]\n      severity: low"},
		{"        - opaque", ""},
		{"max: 1000", "max: 1001"},
		{"name: file-length-limit", "name: other-limit"},
		{"arguments: [20]", "arguments: [21]"},
		{"name: max-public-structs", "name: other-structs"},
	}
	for _, change := range changes {
		altered := strings.Replace(string(config), change[0], change[1], 1)
		if altered == string(config) {
			t.Fatalf("control not applied: %q", change[0])
		}

		problems, parseErr := repopolicy.LintConfigIssues(registry.Exceptions, []byte(altered))
		if parseErr == nil && len(problems) == 0 {
			t.Errorf("filter accepted: %q", change[1])
		}
	}

	_, err = repopolicy.LintConfigIssues(registry.Exceptions, append(config, []byte("\n---\nlinters: {}\n")...))
	if err == nil {
		t.Fatal("multiple configuration documents accepted")
	}
}

// TestStalenessMatchesDiagnosticSource requires the complete actual suppression predicate.
func TestStalenessMatchesDiagnosticSource(t *testing.T) {
	t.Parallel()

	entry := &repopolicy.Entry{
		Tool: repopolicy.ToolLint, Effect: repopolicy.EffectExcludeDiagnostic,
		Linter: "gosec", Path: directiveSourcePath, Message: "specific diagnostic", Source: `os\.ReadFile\(name\)`,
	}

	issues := []repopolicy.Issue{{Linter: "gosec", File: directiveSourcePath, Text: "specific diagnostic", Source: "os.ReadFile(other)"}}
	if len(repopolicy.StaleDiagnosticEntries([]*repopolicy.Entry{entry}, issues, "darwin")) != 1 {
		t.Fatal("unmatched source counted as current")
	}

	issues[0].Source = " os.ReadFile(name)"
	if len(repopolicy.StaleDiagnosticEntries([]*repopolicy.Entry{entry}, issues, "darwin")) != 0 {
		t.Fatal("matching source counted as stale")
	}
}
