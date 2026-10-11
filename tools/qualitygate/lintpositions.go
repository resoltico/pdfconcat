// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	positionControlFile = "internal/capture/actual.go"
)

// lintPositionControls proves compiled display mappings cannot evade physical source ownership.
func lintPositionControls(ctx context.Context, root, binary string) ([]string, error) {
	scratch, err := os.MkdirTemp("", "qualitygate-lint-positions-")
	if err != nil {
		return nil, err
	}
	defer removeAll(scratch)
	if err := prepareLintControls(root, scratch); err != nil {
		return nil, err
	}
	source := "package capture\n//line /tmp/display.go:700:9\nimport \"os\"\nfunc Read(name string)([]byte,error){return os.ReadFile(name)}\n"
	if err := writeArchitectureSource(scratch, positionControlFile, source); err != nil {
		return nil, err
	}
	if err := compileArchitectureControl(ctx, scratch, ""); err != nil {
		return nil, err
	}
	violations, err := repopolicy.ScanDirectives(ctx, scratch)
	if err != nil {
		return nil, err
	}
	if len(violations) != 1 || violations[0].File != positionControlFile || violations[0].Line != 2 {
		return []string{"compiled display mapping escaped physical source ownership guard"}, nil
	}
	return nil, nil
}

func positionControlIssues(ctx context.Context, root, binary string) ([]repopolicy.Issue, error, error) {
	reportFile := filepath.Join(root, lintReportFileName)
	if err := prepareLintRunReport(root); err != nil {
		return nil, nil, err
	}

	args := []string{
		runVerb,
		parallelLintRunners,
		lintNoFixFlag,
		configFlag,
		filepath.Join(root, lintConfigFileName),
		enableOnlyFlag,
		"gosec,errcheck,unused",
		"--output.json.path=" + reportFile,
		allPackages,
	}

	checker := &command{
		dir: root, name: binary, args: args,
		env: []string{"GOLANGCI_LINT_CACHE=" + filepath.Join(root, lintFixtureCache)},
	}
	output, runErr := checker.output(ctx)

	if runErr != nil && exitCode(runErr) != exitIssuesFound {
		return nil, runErr, fmt.Errorf("position control infrastructure: %w\n%s", runErr, output)
	}

	data, err := readLintRunReport(root, runErr, output)
	if err != nil {
		return nil, runErr, err
	}

	issues, err := repopolicy.ParseIssues(data, root)

	return issues, runErr, err
}
