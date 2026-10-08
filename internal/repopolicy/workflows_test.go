// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	toolVersionsPath   = "tools/versions.env"
	gremlinsVersionEnv = "GREMLINS_VERSION"
	versionEnv         = "GOLANGCI_LINT_VERSION"
	wantNotPinned      = "not pinned to a full commit SHA"
	compositeAction    = `name: Example action
description: x
runs:
  using: composite
  steps:
    - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
      with:
        go-version-file: go.mod
    - uses: ./.github/actions/other
    - shell: bash
      run: echo ok
`
	goodWorkflow = `name: Example
on: [push]
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - uses: golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a # v9.3.0
        with:
          version: ${{ env.GOLANGCI_LINT_VERSION }}
      - run: go test ./...
`
)

func readWorkflows(t *testing.T) map[string][]byte {
	t.Helper()

	var paths []string

	for _, pattern := range []string{"workflows/*.yml", "actions/*/action.yml"} {
		matches, err := filepath.Glob(filepath.Join(repoRoot(t), ".github", filepath.FromSlash(pattern)))
		if err != nil {
			t.Fatal(err)
		}

		paths = append(paths, matches...)
	}

	if len(paths) < 3 {
		t.Fatalf("found only %d workflow and action files; the patterns have drifted", len(paths))
	}

	files := map[string][]byte{}

	for _, path := range paths {
		content, readErr := readRepoFileAt(path)
		if readErr != nil {
			t.Fatal(readErr)
		}

		files[strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(repoRoot(t))+"/.github/")] = content
	}

	return files
}

func TestRepositoryWorkflowsMeetPolicy(t *testing.T) {
	t.Parallel()

	problems := repopolicy.WorkflowIssues(readWorkflows(t))
	if len(problems) > 0 {
		t.Fatalf("workflow policy violations:\n%s", strings.Join(problems, "\n"))
	}
}

func TestWorkflowIssuesAcceptsGoodWorkflow(t *testing.T) {
	t.Parallel()

	problems := repopolicy.WorkflowIssues(map[string][]byte{"good.yml": []byte(goodWorkflow)})
	if len(problems) != 0 {
		t.Fatalf(unexpectedProblemsFormat, problems)
	}
}

// TestWorkflowIssuesRejectsEveryViolation is the negative control for the workflow policy.
func TestWorkflowIssuesRejectsEveryViolation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		anchor string
		with   string
		want   string
	}{
		{
			"mutable tag",
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
			"actions/checkout@v7",
			wantNotPinned,
		},
		{
			"short sha",
			"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1",
			"actions/checkout@3d3c42e",
			wantNotPinned,
		},
		{
			"branch",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0",
			"actions/setup-go@main # v7",
			wantNotPinned,
		},
		{
			"pin without tag comment",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0",
			"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
			"trailing comment naming the release tag",
		},
		{"no permissions", "permissions:\n  contents: read\n", "", "no top-level permissions"},
		{"credentials persisted", "persist-credentials: false", "persist-credentials: true", "persist-credentials: false"},
		{"go version literal", "go-version-file: go.mod", "go-version: '1.27.1'", "must read the Go version from go.mod"},
		{"linter version literal", "${{ env.GOLANGCI_LINT_VERSION }}", "v2.14.0", "version must be ${{ env.GOLANGCI_LINT_VERSION }}"},
		{"literal tool version in script", "run: go test ./...", "run: go run example.test/tool@v1.2.3", "literal tool version"},
		{"pull_request_target", "on: [push]", "on:\n  pull_request_target:", "pull_request_target"},
		{
			"remote reusable workflow",
			"    runs-on: ubuntu-latest\n",
			"    uses: owner/repo/.github/workflows/x.yml@main\n",
			"must be local",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			broken := mustReplace(t, goodWorkflow, test.anchor, test.with)

			problems := repopolicy.WorkflowIssues(map[string][]byte{"broken.yml": []byte(broken)})
			requireContains(t, problems, test.want)
		})
	}
}

func TestToolVersionsAreConsistent(t *testing.T) {
	t.Parallel()

	versions, err := repopolicy.ParseToolVersions(string(readRepoFile(t, toolVersionsPath)))
	if err != nil {
		t.Fatal(err)
	}

	problems := repopolicy.ToolVersionIssues(versions, string(readRepoFile(t, ".goreleaser.yml")))
	if len(problems) > 0 {
		t.Fatalf("tool version copies disagree:\n%s", strings.Join(problems, "\n"))
	}
}

func TestToolVersionIssuesRejectsDrift(t *testing.T) {
	t.Parallel()

	good := map[string]string{
		versionEnv: "v2.14.0", "GORELEASER_VERSION": "v2.18.2", "GOVULNCHECK_VERSION": "v1.8.0",
		gremlinsVersionEnv: "v0.6.0", "GITLEAKS_VERSION": "v8.30.1", "ACTIONLINT_VERSION": "v1.7.12",
	}
	config := "# yaml-language-server: $schema=https://raw.githubusercontent.com/goreleaser/goreleaser/v2.18.2/www/static/schema.json\n"

	if problems := repopolicy.ToolVersionIssues(good, config); len(problems) != 0 {
		t.Fatalf(unexpectedProblemsFormat, problems)
	}

	requireContains(
		t,
		repopolicy.ToolVersionIssues(good, strings.Replace(config, "v2.18.2", "v2.17.0", 1)),
		"schema reference must point at",
	)

	missing := map[string]string{versionEnv: "latest"}
	for key, value := range good {
		if key != versionEnv && key != "GREMLINS_VERSION" {
			missing[key] = value
		}
	}

	problems := repopolicy.ToolVersionIssues(missing, config)
	requireContains(t, problems, `GOLANGCI_LINT_VERSION="latest" is not an exact`)
	requireContains(t, problems, `GREMLINS_VERSION="" is not an exact`)
}

func TestParseToolVersionsRejectsMalformedLines(t *testing.T) {
	t.Parallel()

	_, err := repopolicy.ParseToolVersions("GOOD=v1.0.0\nBAD LINE\n")
	if err == nil {
		t.Fatal("malformed line accepted")
	}

	for _, content := range []string{"KEY=one\nKEY=two\n", "bad-key=value\n", "KEY =value\n", "KEY=\n"} {
		if _, parseErr := repopolicy.ParseToolVersions(content); !errors.Is(parseErr, repopolicy.ErrToolVersions) {
			t.Fatalf("invalid environment accepted: %q: %v", content, parseErr)
		}
	}

	versions, err := repopolicy.ParseToolVersions("# comment\n\nKEY=v1.2.3\n")
	if err != nil || versions["KEY"] != "v1.2.3" {
		t.Fatalf("got %v, %v", versions, err)
	}
}

// TestWorkflowIssuesChecksCompositeActionSteps proves that composite actions are held to the same
// pinning rules as workflows, without needing permissions of their own.
func TestWorkflowIssuesChecksCompositeActionSteps(t *testing.T) {
	t.Parallel()

	problems := repopolicy.WorkflowIssues(map[string][]byte{"actions/x/action.yml": []byte(compositeAction)})
	if len(problems) != 0 {
		t.Fatalf(unexpectedProblemsFormat, problems)
	}

	broken := mustReplace(t, compositeAction, "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0", "v7")

	problems = repopolicy.WorkflowIssues(map[string][]byte{"actions/x/action.yml": []byte(broken)})
	requireContains(t, problems, wantNotPinned)
}

// readRepoFileAt reads an absolute path below the repository root through an [os.Root].
func readRepoFileAt(path string) ([]byte, error) {
	dir, name := filepath.Split(path)

	tree, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dir, err)
	}

	content, readErr := tree.ReadFile(name)
	if readErr != nil {
		readErr = fmt.Errorf("read %s: %w", path, readErr)
	}

	return content, errors.Join(readErr, tree.Close())
}
