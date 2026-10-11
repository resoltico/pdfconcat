// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// lintBoundaryControls proves the pinned binary enforces security and file-responsibility settings.
// The fixtures use the actual project configuration, with only the two relevant analyzers enabled
// to keep unrelated diagnostics from concealing whether each oracle found its deliberate defect.
func lintBoundaryControls(ctx context.Context, root, binary string) ([]string, error) {
	scratch, err := os.MkdirTemp("", "qualitygate-lint-controls-")
	if err != nil {
		return nil, fmt.Errorf("create lint controls: %w", err)
	}
	defer removeAll(scratch)

	err = prepareLintControls(root, scratch)
	if err != nil {
		return nil, err
	}

	prefix := "package lintcontrols\nimport \"os\"\nfunc load(name string) ([]byte,error) {\n"
	suffix := "return os.ReadFile(name)\n}\n"

	positive, err := lintControlIssues(ctx, scratch, binary, prefix+suffix)
	if err != nil {
		return nil, err
	}

	marker := "#" + "no" + "sec G304"
	suppressedSource := prefix + "/* rationale\n" + marker + "\n*/\n" + suffix

	suppressed, err := lintControlIssues(ctx, scratch, binary, suppressedSource)
	if err != nil {
		return nil, err
	}

	violations, err := repopolicy.DirectivesIn("fixture.go", []byte(suppressedSource))
	if err != nil {
		return nil, fmt.Errorf("scan lint suppression control: %w", err)
	}

	problems := securityControlProblems(positive, suppressed, violations)

	fallible, err := lintSelectedIssues(ctx, scratch, binary,
		"package lintcontrols\nimport (\"fmt\";\"io\";\"os\")\nfunc arbitrary(w io.Writer){fmt.Fprintf(w, \"value\")}\nfunc file(w *os.File){fmt.Fprintf(w, \"value\")}\n", "errcheck")
	if err != nil {
		return nil, err
	}
	if countLintIssues(fallible, "errcheck", "fmt.Fprintf") != 2 {
		problems = append(problems, "errcheck did not reject both fallible writer sinks")
	}
	return problems, nil
}

func lintControlIssues(ctx context.Context, scratch, binary, source string) ([]repopolicy.Issue, error) {
	return lintSelectedIssues(ctx, scratch, binary, source, "gosec,errcheck")
}

func lintSelectedIssues(ctx context.Context, scratch, binary, source, linters string) ([]repopolicy.Issue, error) {
	err := os.WriteFile(filepath.Join(scratch, "fixture.go"), []byte(source), fileMode)
	if err != nil {
		return nil, fmt.Errorf("write lint fixture: %w", err)
	}

	reportFile := filepath.Join(scratch, lintReportFileName)
	if prepareErr := prepareLintRunReport(scratch); prepareErr != nil {
		return nil, prepareErr
	}

	// Independent fixtures own their source, report and analyzer cache; the upstream machine-wide
	// runner lock would serialize unrelated controls even when their Go tests run in parallel.
	output, runErr := (&command{
		dir: scratch, name: binary,
		env: []string{"GOLANGCI_LINT_CACHE=" + filepath.Join(scratch, lintFixtureCache)}, args: []string{
			runVerb, parallelLintRunners, lintNoFixFlag, configFlag, filepath.Join(scratch, scratchLintConfigName),
			enableOnlyFlag, linters,
			"--output.json.path=" + reportFile, allPackages,
		},
	}).output(ctx)
	if runErr != nil && exitCode(runErr) != exitIssuesFound {
		return nil, fmt.Errorf("lint control infrastructure: %w\n%s", runErr, output)
	}

	data, err := readLintRunReport(scratch, runErr, output)
	if err != nil {
		return nil, err
	}

	issues, err := repopolicy.ParseIssues(data, scratch)
	if err != nil {
		return nil, fmt.Errorf("parse lint control: %w", err)
	}

	return issues, nil
}

func hasLintIssue(issues []repopolicy.Issue, linter, message string) bool {
	for _, issue := range issues {
		if issue.Linter == linter && strings.Contains(issue.Text, message) {
			return true
		}
	}

	return false
}

func prepareLintControls(root, scratch string) error {
	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return err
	}

	for name, data := range map[string][]byte{
		moduleFileName: []byte("module lintcontrols\n"), scratchLintConfigName: config,
	} {
		err = os.WriteFile(filepath.Join(scratch, name), data, fileMode)
		if err != nil {
			return fmt.Errorf("write lint control: %w", err)
		}
	}

	return nil
}

func securityControlProblems(positive, suppressed []repopolicy.Issue, violations []repopolicy.DirectiveViolation) []string {
	var problems []string
	if !hasLintIssue(positive, securityLinter, "G304:") {
		problems = append(problems, "real gosec did not reject the unexcepted file read")
	}

	if hasLintIssue(suppressed, securityLinter, "G304:") {
		problems = append(problems, "real multiline suppression control did not suppress G304")
	}

	if len(violations) != 1 || violations[0].Kind != "nosec" {
		problems = append(problems, "comment guard accepted the active multiline suppression")
	}

	return problems
}

func countLintIssues(issues []repopolicy.Issue, linter, message string) int {
	count := 0

	for _, issue := range issues {
		if issue.Linter == linter && strings.Contains(issue.Text, message) {
			count++
		}
	}

	return count
}
