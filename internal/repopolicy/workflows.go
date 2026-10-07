// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

var (
	// ErrToolVersions reports a malformed tools/versions.env.
	ErrToolVersions = errors.New("tool versions")

	commitPin          = regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
	literalVersion     = regexp.MustCompile(`@v\d+\.\d+`)
	exactVersion       = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	toolEnvironmentKey = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// WorkflowIssues checks the properties of GitHub workflow files that review would otherwise have to
// remember: third-party actions are pinned to a full commit with the release tag noted beside it,
// every workflow states its permissions, checkout does not persist credentials, the Go toolchain
// comes from go.mod, tool versions come from tools/versions.env rather than literals, and nothing
// runs with a pull request's secrets. files maps a path (a workflow or a composite action file) to its
// content; a file with a top-level runs key is a composite action and is checked step by step only.
func WorkflowIssues(files map[string][]byte) []string {
	var problems []string

	for _, name := range slices.Sorted(maps.Keys(files)) {
		var document yaml.Node

		err := yaml.Unmarshal(files[name], &document)
		if err != nil || len(document.Content) == 0 {
			problems = append(problems, fmt.Sprintf("%s: not valid YAML: %v", name, err))

			continue
		}

		root := document.Content[0]

		if runs := mappingValue(root, "runs"); runs != nil {
			problems = append(problems, stepsIssues(name, mappingValue(runs, "steps"))...)
		} else {
			problems = append(problems, checkWorkflow(name, root)...)
		}
	}

	return problems
}

// checkWorkflow checks a workflow file's top-level structure and every job.
func checkWorkflow(name string, root *yaml.Node) []string {
	var problems []string

	if mappingValue(root, "permissions") == nil {
		problems = append(problems, name+": no top-level permissions; declare the least privilege every job needs")
	}

	if mappingValue(mappingValue(root, "on"), "pull_request_target") != nil {
		problems = append(problems, name+": pull_request_target runs with repository secrets on untrusted changes")
	}

	jobs := mappingValue(root, "jobs")
	if jobs == nil {
		return append(problems, name+": no jobs")
	}

	for index := 1; index < len(jobs.Content); index += pairWidth {
		problems = append(problems, jobIssues(name, jobs.Content[index-1].Value, jobs.Content[index])...)
	}

	return problems
}

func jobIssues(file, job string, node *yaml.Node) []string {
	var problems []string

	where := file + " job " + job

	uses := mappingValue(node, "uses")
	if uses != nil && !strings.HasPrefix(uses.Value, "./") {
		problems = append(problems, where+": reusable workflow "+uses.Value+" must be local")
	}

	return append(problems, stepsIssues(where, mappingValue(node, "steps"))...)
}

func stepsIssues(where string, steps *yaml.Node) []string {
	if steps == nil {
		return nil
	}

	problems := make([]string, 0, len(steps.Content))

	for position, step := range steps.Content {
		problems = append(problems, stepIssues(fmt.Sprintf("%s step %d", where, position+1), step)...)
	}

	return problems
}

func stepIssues(where string, step *yaml.Node) []string {
	var problems []string

	if run := mappingValue(step, "run"); run != nil && literalVersion.MatchString(run.Value) {
		problems = append(problems, where+": run step carries a literal tool version; read it from tools/versions.env")
	}

	used := mappingValue(step, "uses")
	if used == nil || strings.HasPrefix(used.Value, "./") {
		return problems
	}

	if !commitPin.MatchString(used.Value) {
		problems = append(problems, where+": "+used.Value+" is not pinned to a full commit SHA")
	}

	if !strings.HasPrefix(used.LineComment, "# v") {
		problems = append(problems, where+": "+used.Value+" needs a trailing comment naming the release tag, such as # v1.2.3")
	}

	return append(problems, actionInputIssues(where, used.Value, mappingValue(step, "with"))...)
}

// actionInputIssues checks the inputs of the actions whose inputs carry policy.
func actionInputIssues(where, action string, with *yaml.Node) []string {
	var problems []string

	switch {
	case strings.HasPrefix(action, "actions/setup-go@"):
		if value(with, "go-version-file") != "go.mod" {
			problems = append(problems, where+": setup-go must read the Go version from go.mod (go-version-file: go.mod)")
		}

		if value(with, "go-version") != "" {
			problems = append(problems, where+": setup-go must not repeat the Go version; go.mod is the one source")
		}
	case strings.HasPrefix(action, "actions/checkout@"):
		if value(with, "persist-credentials") != "false" {
			problems = append(problems, where+": checkout must set persist-credentials: false")
		}
	case strings.HasPrefix(action, "golangci/golangci-lint-action@"):
		if value(with, "version") != "${{ env.GOLANGCI_LINT_VERSION }}" {
			problems = append(problems, where+": golangci-lint version must be ${{ env.GOLANGCI_LINT_VERSION }}")
		}
	case strings.HasPrefix(action, "goreleaser/goreleaser-action@"):
		if value(with, "version") != "${{ env.GORELEASER_VERSION }}" {
			problems = append(problems, where+": goreleaser version must be ${{ env.GORELEASER_VERSION }}")
		}
	default:
	}

	return problems
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for index := 0; index+1 < len(node.Content); index += pairWidth {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}

	return nil
}

func value(node *yaml.Node, key string) string {
	found := mappingValue(node, key)
	if found == nil {
		return ""
	}

	return found.Value
}

// ParseToolVersions parses the KEY=VALUE lines of tools/versions.env; blank lines and # comments are ignored.
func ParseToolVersions(content string) (map[string]string, error) {
	versions := map[string]string{}

	for number, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, setting, found := strings.Cut(line, "=")
		if !found || !toolEnvironmentKey.MatchString(key) || setting == "" {
			return nil, fmt.Errorf("%w: line %d: want KEY=VALUE, got %q", ErrToolVersions, number+1, line)
		}

		if _, duplicate := versions[key]; duplicate {
			return nil, fmt.Errorf("%w: line %d: duplicate key %s", ErrToolVersions, number+1, key)
		}

		versions[key] = setting
	}

	return versions, nil
}

// ToolVersionIssues checks that tools/versions.env, the one source of tool versions, defines every
// tool and that the copies derived from it agree: the GoReleaser configuration's schema reference.
func ToolVersionIssues(versions map[string]string, goreleaserConfig string) []string {
	var problems []string

	for _, key := range []string{
		"GOLANGCI_LINT_VERSION", "GORELEASER_VERSION", "GOVULNCHECK_VERSION", "GREMLINS_VERSION", "ACTIONLINT_VERSION", "GITLEAKS_VERSION",
	} {
		if !exactVersion.MatchString(versions[key]) {
			problems = append(problems, fmt.Sprintf("tools/versions.env: %s=%q is not an exact vX.Y.Z version", key, versions[key]))
		}
	}

	want := "goreleaser/" + versions["GORELEASER_VERSION"] + "/www/static/schema.json"
	if !strings.Contains(goreleaserConfig, want) {
		problems = append(problems, ".goreleaser.yml: the schema reference must point at "+want)
	}

	return problems
}
