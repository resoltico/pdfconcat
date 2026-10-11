// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestMutationRegistryAcceptsDefendedExactJudgment(t *testing.T) {
	registry, err := repopolicy.ParseRegistry([]byte(registryHeader + mutationEntry))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.For(repopolicy.ToolMutation)) != 1 || registry.Exceptions[0].ID != "mutation-example" || !strings.Contains(registry.Exceptions[0].Rationale, "repair would contradict") {
		t.Fatalf("lost judgment or rationale: %+v", registry)
	}
}

func TestMutationRegistryRejectsBroadMissingAndUnresolvedEntries(t *testing.T) {
	for _, change := range []struct{ name, old, replacement string }{
		{"missing rationale", rationaleComment, ""},
		{"short rationale", rationaleComment, "  # because\n"},
		{"bad id", "mutation-example", "Bad_ID"},
		{"wrong tool", "tool: mutation", "tool: golangci-lint"},
		{"wrong kind", "equivalent-mutant", "intended-pattern"},
		{"short property", "both sides behave identically", "ok"},
		{"unknown field", "    operator:", "    linter: gosec\n    operator:"},
		{"glob path", examplePath, "internal/example/*.go"},
		{"test-wide path", examplePath, "internal/*_test.go"},
		{"directory", examplePath, "internal/example"},
		{"parent path", examplePath, "../file.go"},
		{"absolute path", examplePath, "/file.go"},
		{"regex path", examplePath, "internal/(a|b).go"},
		{"unknown operator", "CONDITIONALS_BOUNDARY", "ALL"},
		{"missing anchor", "    anchor: if count > 0 {\n", ""},
		{"untrimmed anchor", "anchor: if count > 0 {", "anchor: '  if count > 0 {'"},
		{"missing column", "    column: 10\n", ""},
		{"zero column", "column: 10", "column: 0"},
		{"fractional column", "column: 10", "column: 1.5"},
		{"missing statuses", "    statuses: [lived]\n", ""},
		{"unresolved status", "statuses: [lived]", "statuses: [not-covered]"},
	} {
		t.Run(change.name, func(t *testing.T) {
			input := registryHeader + mustReplace(t, mutationEntry, change.old, change.replacement)
			if _, err := repopolicy.ParseRegistry([]byte(input)); err == nil {
				t.Fatal("invalid judgment accepted")
			}
		})
	}
}

func TestMutationRegistryRejectsRetiredAuthoritiesAndInvalidDocuments(t *testing.T) {
	good := registryHeader + mutationEntry
	for _, input := range []string{
		good + mutationEntry, good + "\n---\n", good + "\n---\nunknown: true\n", "exceptions: [unclosed", "version: 2\nexceptions: []\n", registryHeader + "  - null\n",
		"version: 1\ncoverage_threshold_percent: 100\nexceptions: []\n",
		good + "    source: ReadFile\n", good + "    message: G304\n", good + "    setting: disabled-check\n", good + "    goos: windows\n", good + "    function: Run\n",
	} {
		if _, err := repopolicy.ParseRegistry([]byte(input)); err == nil {
			t.Fatalf("invalid document accepted: %q", input)
		}
	}
}
