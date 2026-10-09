// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// SourceOwners derives exact production and test-support directory classifications from native depguard rules.
// Import matching remains depguard's responsibility; this only validates the declaration shape.
func SourceOwners(config []byte, module string) (map[string]string, error) {
	document, err := decodeLintConfig(config)
	if err != nil {
		return nil, err
	}

	rules, ok := lookup(document, "linters.settings.depguard.rules").(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: missing depguard rules", ErrLintConfig)
	}

	owners := map[string]string{}

	for name, value := range rules {
		if !sourceOwnerRule(name) {
			continue
		}

		rule, isRule := value.(map[string]any)
		if !isRule {
			return nil, fmt.Errorf("%w: invalid source owner rule %s", ErrLintConfig, name)
		}

		dirs, scopeErr := sourceOwnerDirectories(name, rule, module)
		if scopeErr != nil {
			return nil, scopeErr
		}

		for _, dir := range dirs {
			if previous, exists := owners[dir]; exists {
				return nil, fmt.Errorf("%w: ambiguous source owner %s: %s and %s", ErrLintConfig, dir, previous, name)
			}

			owners[dir] = name
		}
	}

	if len(owners) == 0 {
		return nil, fmt.Errorf("%w: no production or test-support internal-boundary rules", ErrLintConfig)
	}

	return owners, nil
}

func sourceOwnerDirectories(name string, rule map[string]any, module string) ([]string, error) {
	files := activeItems(rule["files"])
	if rule["list-mode"] != "lax" || !slices.Contains(files, "!$test") || !deniesProject(rule, module) {
		return nil, fmt.Errorf("%w: %s must be lax, exclude tests and deny the project prefix", ErrLintConfig, name)
	}

	for _, allow := range activeItems(rule["allow"]) {
		if !strings.HasPrefix(allow, module+"/") || !strings.HasSuffix(allow, "$") {
			return nil, fmt.Errorf("%w: %s must allow internal packages exactly", ErrLintConfig, name)
		}
	}

	dirs := make([]string, 0, len(files))

	for _, selector := range files {
		if selector == "!$test" {
			continue
		}

		dir, err := sourceOwnerDirectory(name, selector)
		if err != nil {
			return nil, err
		}

		dirs = append(dirs, dir)
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("%w: %s selects no owned directories", ErrLintConfig, name)
	}

	return dirs, nil
}

func deniesProject(rule map[string]any, module string) bool {
	denied, ok := rule["deny"].([]any)
	if !ok {
		return false
	}

	for _, item := range denied {
		value, isMapping := item.(map[string]any)
		if isMapping && value["pkg"] == module {
			return true
		}
	}

	return false
}

// SourceClassificationIssues requires every owned file to compile for a supported variant and every
// non-test Go source to have one production or test-support owner. Tests retain their import policy.
func SourceClassificationIssues(owners map[string]string, files []string, compiled map[string]bool) []string {
	var problems []string

	production := 0

	for _, file := range files {
		if !compiled[file] {
			problems = append(problems, "owned file is excluded from every supported target: "+file)
		}

		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		owner := owners[path.Dir(file)]
		if !strings.HasPrefix(owner, "test-support-") {
			production++
		}

		if owner == "" {
			problems = append(problems, "unclassified non-test file: "+file)
		}
	}

	if production == 0 {
		problems = append(problems, "no owned production files discovered")
	}

	slices.Sort(problems)

	return problems
}

func sourceOwnerDirectory(name, selector string) (string, error) {
	dir, hasPrefix := strings.CutPrefix(selector, "**/")
	if !hasPrefix {
		return "", fmt.Errorf("%w: %s has unsupported production selector %q", ErrLintConfig, name, selector)
	}

	dir, hasSuffix := strings.CutSuffix(dir, "/*.go")
	if !hasSuffix || dir == "." || dir == "" || path.Clean(dir) != dir || strings.ContainsAny(dir, "*?![]{}\\") ||
		strings.HasPrefix(dir, "../") {
		return "", fmt.Errorf("%w: %s must select an exact production directory", ErrLintConfig, name)
	}

	return dir, nil
}

func sourceOwnerRule(name string) bool {
	return strings.HasPrefix(name, "production-") || strings.HasPrefix(name, "test-support-")
}
