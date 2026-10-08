// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// ProductionOwners derives exact production-directory classifications from native depguard rules.
// Import matching remains depguard's responsibility; this only validates the declaration shape.
func ProductionOwners(config []byte, module string) (map[string]string, error) {
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
		if !strings.HasPrefix(name, "production-") {
			continue
		}

		rule, isRule := value.(map[string]any)
		if !isRule {
			return nil, fmt.Errorf("%w: invalid production rule %s", ErrLintConfig, name)
		}

		dirs, scopeErr := productionDirectories(name, rule, module)
		if scopeErr != nil {
			return nil, scopeErr
		}

		for _, dir := range dirs {
			if previous, exists := owners[dir]; exists {
				return nil, fmt.Errorf("%w: ambiguous production owner %s: %s and %s", ErrLintConfig, dir, previous, name)
			}

			owners[dir] = name
		}
	}

	if len(owners) == 0 {
		return nil, fmt.Errorf("%w: no production internal-boundary rules", ErrLintConfig)
	}

	return owners, nil
}

func productionDirectories(name string, rule map[string]any, module string) ([]string, error) {
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

		dir, err := productionDirectory(name, selector)
		if err != nil {
			return nil, err
		}

		dirs = append(dirs, dir)
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("%w: %s selects no production directories", ErrLintConfig, name)
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

// ProductionClassificationIssues requires every owned file to compile for at least one supported target
// and each production file to have one configured owner. Tests retain native depguard's separate import policy.
func ProductionClassificationIssues(owners map[string]string, files []string, compiled map[string]bool) []string {
	var problems []string

	production := 0

	for _, file := range files {
		if !compiled[file] {
			problems = append(problems, "owned file is excluded from every supported target: "+file)
		}

		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		production++

		if owners[path.Dir(file)] == "" {
			problems = append(problems, "unclassified production file: "+file)
		}
	}

	if production == 0 {
		problems = append(problems, "no owned production files discovered")
	}

	slices.Sort(problems)

	return problems
}

func productionDirectory(name, selector string) (string, error) {
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
