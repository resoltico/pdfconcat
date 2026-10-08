// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type (
	formatterSelection struct {
		Enable []string `yaml:"enable"`
	}

	formatterSelectionDocument struct {
		Formatters formatterSelection `yaml:"formatters"`
	}
)

const formatterSelectionSetting = "formatters.enable"

func TestFormatterSelectionPreservesEveryConfiguredFormatter(t *testing.T) {
	t.Parallel()

	var document formatterSelectionDocument
	if err := yaml.Unmarshal(readRepoFile(t, golangciFile), &document); err != nil {
		t.Fatal(err)
	}

	configured := document.Formatters.Enable
	if len(configured) == 0 {
		t.Fatal("formatter selection is empty")
	}

	for index, formatter := range configured {
		t.Run("missing "+formatter, func(t *testing.T) {
			t.Parallel()

			selected := append([]string{}, configured[:index]...)
			selected = append(selected, configured[index+1:]...)
			assertConfigSetting(t, formatterSelectionSetting, selected, "formatters.enable must contain exactly")
		})
	}

	for _, selected := range [][]string{nil, append(append([]string{}, configured...), configured[0]), {"unknown-formatter"}} {
		assertConfigSetting(t, formatterSelectionSetting, selected, "formatters.enable must contain exactly")
	}

	reordered := append([]string{}, configured...)
	for left, right := 0, len(reordered)-1; left < right; left, right = left+1, right-1 {
		reordered[left], reordered[right] = reordered[right], reordered[left]
	}

	assertConfigSetting(t, formatterSelectionSetting, reordered, "")
}

func TestLintActionsCannotRewriteTheirInputs(t *testing.T) {
	t.Parallel()

	assertConfigSetting(t, "issues.fix", true, "issues.fix must be absent or false")
	assertConfigSetting(t, "issues.fix", false, "")
	assertConfigSetting(t, "formatters.settings.gofmt.rewrite-rules",
		[]map[string]string{{"pattern": "a + 0", "replacement": "a + 1"}}, "semantic rewrites are unsupported")
	assertConfigSetting(t, "formatters.settings.gofmt.rewrite-rules", []any{}, "")
}

func TestNativeSuppressionAliasesRequireRegistryEntries(t *testing.T) {
	t.Parallel()

	for setting, value := range map[string]any{
		"linters.settings.gosmopolitan.escape-hatches":            []string{"example.org/policy.Accept"},
		"linters.settings.gosmopolitan.allow-time-local":          true,
		"linters.settings.staticcheck.dot-import-whitelist":       []string{"fmt"},
		"linters.settings.staticcheck.http-status-code-whitelist": []string{"201"},
		"linters.settings.unqueryvet.allowed-patterns":            []string{"SELECT.*"},
	} {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			assertConfigSetting(t, setting, value, "the registry does not list: "+setting)
		})
	}

	assertConfigSetting(t, "linters.settings.gosmopolitan.escape-hatches", []any{}, "")
	assertConfigSetting(t, "linters.settings.gosmopolitan.allow-time-local", false, "")
}

func assertConfigSetting(t *testing.T, dotted string, value any, want string) {
	t.Helper()

	var document map[string]any
	if err := yaml.Unmarshal(readRepoFile(t, golangciFile), &document); err != nil {
		t.Fatal(err)
	}

	keys := strings.Split(dotted, ".")
	current := document

	for _, key := range keys[:len(keys)-1] {
		next, exists := current[key].(map[string]any)
		if !exists {
			next = map[string]any{}
			current[key] = next
		}

		current = next
	}

	current[keys[len(keys)-1]] = value

	config, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	problems, err := repopolicy.LintConfigIssues(loadRegistry(t).Exceptions, config)
	if err != nil {
		t.Fatal(err)
	}

	if want == "" && len(problems) != 0 || want != "" && !strings.Contains(strings.Join(problems, "\n"), want) {
		t.Fatalf("setting %s: problems=%v, want %q", dotted, problems, want)
	}
}
