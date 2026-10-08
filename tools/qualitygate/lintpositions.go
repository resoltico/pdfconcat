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

const (
	positionControlFile = "internal/capture/actual.go"
	readCallLine        = 4
)

// lintPositionControls proves valid compiler display directives cannot change diagnostic predicates.
func lintPositionControls(ctx context.Context, root, binary string) ([]string, error) {
	scratch, err := os.MkdirTemp("", "qualitygate-lint-positions-")
	if err != nil {
		return nil, fmt.Errorf("create lint position controls: %w", err)
	}
	defer removeAll(scratch)

	for _, file := range []string{moduleFileName, "go.sum", lintConfigFileName} {
		data, readErr := readInRoot(root, file)
		if readErr != nil {
			return nil, readErr
		}

		if writeErr := writeArchitectureSource(scratch, file, string(data)); writeErr != nil {
			return nil, writeErr
		}
	}

	source := "//line /tmp/display.go:700:9\npackage capture\nimport \"os\"\n" +
		"func Read(name string)([]byte,error){return os.ReadFile(name)}\n" +
		"func Discard(f *os.File){f.Close()}\ntype unusedShape struct{hidden int}\n"
	if writeErr := writeArchitectureSource(scratch, positionControlFile, source); writeErr != nil {
		return nil, writeErr
	}

	if compileErr := compileArchitectureControl(ctx, scratch, ""); compileErr != nil {
		return nil, compileErr
	}

	problems, checkErr := physicalDiagnosticControls(ctx, scratch, binary)
	if checkErr != nil {
		return nil, checkErr
	}

	forged, err := forgedSourcePositionControl(ctx, scratch, binary)
	if err != nil {
		return nil, err
	}

	return append(problems, forged...), nil
}

func positionControlIssues(ctx context.Context, root, binary string) ([]repopolicy.Issue, error, error) {
	reportFile := filepath.Join(root, lintReportFileName)
	if err := prepareLintRunReport(root); err != nil {
		return nil, nil, err
	}

	args := []string{
		runVerb,
		serialLintRunners,
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
		env: []string{"GOLANGCI_LINT_CACHE=" + filepath.Join(root, "cache")},
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

func hasPhysicalIssue(issues []repopolicy.Issue, file, linter, text, source string, line int) bool {
	for _, issue := range issues {
		if issue.File == file && issue.Linter == linter && issue.Line == line && strings.Contains(issue.Text, text) &&
			strings.Contains(issue.Source, source) {
			return true
		}
	}

	return false
}

func forgedSourcePositionControl(ctx context.Context, root, binary string) ([]string, error) {
	source := "//line /tmp/display.go:1\npackage capture\nimport \"os\"\nfunc Read(name string)([]byte,error){return os.ReadFile(name)}\n"
	if err := writeArchitectureSource(root, positionControlFile, source); err != nil {
		return nil, err
	}

	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return nil, err
	}

	forgedRule := `      - linters: [gosec]
        path: ^internal/capture/actual\.go$
        text: 'G304: Potential file inclusion via variable'
        source: 'import "os"'
`

	rewritten := strings.Replace(string(config), "\nformatters:", "\n"+forgedRule+"\nformatters:", 1)
	if rewritten == string(config) {
		return nil, fmt.Errorf("%w: position control exclusion was not inserted", errGate)
	}

	if writeErr := writeArchitectureSource(root, lintConfigFileName, rewritten); writeErr != nil {
		return nil, writeErr
	}

	issues, runErr, err := positionControlIssues(ctx, root, binary)
	if err != nil {
		return nil, err
	}

	if runErr == nil || !hasPhysicalIssue(issues, positionControlFile, securityLinter, "G304", "os.ReadFile(name)", readCallLine) {
		return []string{"adjusted source line forged a diagnostic exclusion"}, nil
	}

	return nil, nil
}

func physicalDiagnosticControls(ctx context.Context, scratch, binary string) ([]string, error) {
	var problems []string

	for range 2 {
		issues, runErr, readErr := positionControlIssues(ctx, scratch, binary)
		if readErr != nil {
			return nil, readErr
		}

		if runErr == nil {
			problems = append(problems, "mapped position defects were not rejected")
		}

		for _, expected := range []struct {
			linter, text, source string
			line                 int
		}{
			{securityLinter, "G304", "os.ReadFile(name)", 4},
			{"errcheck", "f.Close", "f.Close()", 5},
			{"unused", "type unusedShape", "unusedShape", 6},
		} {
			if !hasPhysicalIssue(issues, positionControlFile, expected.linter, expected.text, expected.source, expected.line) {
				problems = append(problems, "mapped physical diagnostic lost: "+expected.linter)
			}
		}
	}

	return problems, nil
}
