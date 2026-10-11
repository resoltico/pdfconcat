// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package repopolicy

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// InfallibleWriterFunctions names concrete memory sinks whose nil-error implementation is checked against the selected SDK.
func InfallibleWriterFunctions() []string {
	return []string{"(*strings.Builder).WriteString", "(*strings.Builder).WriteRune", "(*bytes.Buffer).WriteString", "(*bytes.Buffer).Write", "fmt.Fprintf(*strings.Builder)", "fmt.Fprintf(*bytes.Buffer)"}
}

// CorrectnessProfileIssues rejects hidden suppression or changes to the selected correctness profile.
func CorrectnessProfileIssues(config []byte) ([]string, error) {
	d, err := decodeLintConfig(config)
	if err != nil {
		return nil, err
	}
	var problems []string
	for setting, want := range map[string]any{
		"linters.default":                                      "none",
		"issues.fix":                                           false,
		"run.issues-exit-code":                                 1,
		"linters.exclusions.generated":                         "disable",
		"formatters.exclusions.generated":                      "disable",
		"issues.new":                                           false,
		"issues.max-issues-per-linter":                         0,
		"issues.max-same-issues":                               0,
		"linters.settings.errcheck.check-blank":                true,
		"linters.settings.errcheck.check-type-assertions":      true,
		"linters.settings.errcheck.disable-default-exclusions": true,
		"run.tests":                                            true,
		"run.modules-download-mode":                            "readonly",
		"issues.uniq-by-line":                                  false,
		"issues.new-from-rev":                                  "",
		"issues.new-from-patch":                                "",
		"issues.new-from-merge-base":                           "",
		"linters.settings.gosec.severity":                      "low",
		"linters.settings.gosec.confidence":                    "low",
		"linters.settings.nolintlint.allow-unused":             false,
		"linters.settings.nolintlint.require-explanation":      true,
		"linters.settings.nolintlint.require-specific":         true,
		"linters.settings.unused.field-writes-are-uses":        true,
		"linters.settings.govet.disable-all":                   false,
	} {
		got := lookup(d, setting)
		if got == nil && (setting == "issues.fix" || setting == "run.issues-exit-code" || setting == "linters.settings.govet.disable-all") {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			problems = append(problems, setting+" differs from the reviewed correctness profile")
		}
	}
	for setting, want := range map[string][]string{
		"linters.enable":    {"govet", "staticcheck", "errcheck", "ineffassign", "unused", "gosec", "depguard", "nolintlint"},
		"formatters.enable": {"gofumpt"},
		"linters.settings.errcheck.exclude-functions": InfallibleWriterFunctions(),
		"linters.settings.staticcheck.checks":         {"all", "-ST1000", "-ST1003", "-ST1016"},
	} {
		got := stringList(lookup(d, setting))
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			problems = append(problems, setting+" must contain exactly the reviewed selection")
		}
	}
	for _, setting := range []string{"linters.disable", "linters.exclusions.paths", "linters.exclusions.presets", "formatters.exclusions.paths", "formatters.settings.gofmt.rewrite-rules", "linters.settings.gosec.excludes", "linters.settings.gosec.includes", "linters.settings.govet.disable", "linters.settings.govet.enable", "linters.settings.errcheck.exclude", "run.build-tags", "issues.exclude-files", "issues.exclude-dirs"} {
		if len(asList(lookup(d, setting))) > 0 {
			problems = append(problems, setting+": broad exclusions or semantic rewrites are unsupported")
		}
	}
	for _, raw := range asList(lookup(d, "linters.exclusions.rules")) {
		rule := asMap(raw)
		for key := range rule {
			if key != "path" && key != "linters" && key != "text" {
				problems = append(problems, "unsupported diagnostic predicate: "+key)
			}
		}
		pattern := asString(rule["path"])
		file := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(pattern, "^"), "$"), `\.`, ".")
		if pattern != "^"+regexp.QuoteMeta(file)+"$" || !strings.HasSuffix(file, ".go") || strings.Contains(file, "..") || rule["source"] != nil || len(asList(rule["linters"])) != 1 || asString(rule["text"]) == "" {
			problems = append(problems, fmt.Sprintf("diagnostic suppression must name one exact Go file, linter and diagnostic: %v", raw))
		}
	}
	return problems, nil
}
