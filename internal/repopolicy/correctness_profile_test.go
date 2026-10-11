// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
	"go.yaml.in/yaml/v3"
)

func TestCorrectnessProfileRejectsMissingChecksAndHiddenFilters(t *testing.T) {
	original := readRepoFile(t, golangciFile)
	if problems, err := repopolicy.CorrectnessProfileIssues(original); err != nil || len(problems) != 0 {
		t.Fatalf("owned profile: %v %v", problems, err)
	}
	for _, change := range []struct {
		path  string
		value any
	}{
		{"linters.default", "all"},
		{"linters.enable", []string{"govet"}},
		{"formatters.enable", []string{"gofumpt", "gofmt"}},
		{"run.tests", false},
		{"run.issues-exit-code", 0},
		{"issues.new", true},
		{"issues.new-from-rev", "HEAD~1"},
		{"issues.fix", true},
		{"linters.exclusions.generated", "strict"},
		{"linters.exclusions.paths", []string{"internal/.*"}},
		{"linters.exclusions.presets", []string{"common-false-positives"}},
		{"linters.settings.gosec.excludes", []string{"G304"}},
		{"linters.settings.govet.disable", []string{"printf"}},
		{"linters.settings.govet.disable-all", true},
		{"run.build-tags", []string{"selective"}},
		{"linters.settings.staticcheck.checks", []string{"ST1000"}},
		{"linters.settings.errcheck.check-blank", false},
		{"linters.settings.errcheck.exclude-functions", []string{"fmt.Fprintf"}},
		{"linters.exclusions.rules", []map[string]any{{"linters": []string{"gosec"}, "path": "^internal/.*\\.go$", "text": "G304"}}},
		{"linters.exclusions.rules", []map[string]any{{"linters": []string{"gosec"}, "path": "^internal/app/job.go$", "text": "G304"}}},
		{"linters.exclusions.rules", []map[string]any{{"linters": []string{"gosec"}, "path": "^internal/app/job\\.go$", "text": "G304", "source": "ReadFile"}}},
	} {
		t.Run(change.path, func(t *testing.T) {
			var doc map[string]any
			if err := yaml.Unmarshal(original, &doc); err != nil {
				t.Fatal(err)
			}
			keys := strings.Split(change.path, ".")
			current := doc
			for _, key := range keys[:len(keys)-1] {
				next, ok := current[key].(map[string]any)
				if !ok {
					next = map[string]any{}
					current[key] = next
				}
				current = next
			}
			current[keys[len(keys)-1]] = change.value
			data, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			problems, err := repopolicy.CorrectnessProfileIssues(data)
			if err == nil && len(problems) == 0 {
				t.Fatalf("filter escaped: %s %v", change.path, change.value)
			}
		})
	}
}
