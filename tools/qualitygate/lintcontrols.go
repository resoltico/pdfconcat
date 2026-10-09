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

const reviveLinter = "revive"

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

	limits, limitErr := sourceLimitControls(ctx, scratch, binary)
	if limitErr != nil {
		return nil, limitErr
	}

	problems = append(problems, limits...)

	extra, err := parameterAndWriterControls(ctx, scratch, binary)
	if err != nil {
		return nil, err
	}

	problems = append(problems, extra...)

	paired, err := whitespaceControls(ctx, scratch, binary)
	if err != nil {
		return nil, err
	}

	return append(problems, paired...), nil
}

func lintControlIssues(ctx context.Context, scratch, binary, source string) ([]repopolicy.Issue, error) {
	return lintSelectedIssues(ctx, scratch, binary, source, "revive,gosec,unparam,errcheck")
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

func oversizedSource() string {
	var source strings.Builder
	source.WriteString("package lintcontrols\n")

	for index := range 1001 {
		fmt.Fprintf(&source, "func value%d() {}\n", index)
	}

	for index := range 21 {
		fmt.Fprintf(&source, "type Shape%d struct { Value int }\n", index)
	}

	return source.String()
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

func parameterAndWriterControls(ctx context.Context, scratch, binary string) ([]string, error) {
	var problems []string

	unusedParameter, err := lintControlIssues(
		ctx,
		scratch,
		binary,
		"package main\n"+
			"func consume(ignored int, value int) int { return value+1 }\n"+
			"func Exported(ignored int, value int) int { return value+2 }\n"+
			"func main() { println(consume(3,5));println(consume(4,6));println(Exported(7,8));println(Exported(9,10)) }\n",
	)
	if err != nil {
		return nil, err
	}

	if countLintIssues(unusedParameter, "unparam", "unused") != 2 {
		problems = append(problems, "real unparam did not reject an unused behavior parameter")
	}

	fallibleFormatter, err := lintControlIssues(ctx, scratch, binary,
		"package lintcontrols\nimport (\"fmt\";\"io\";\"os\")\n"+
			"func arbitrary(w io.Writer) { fmt.Fprintf(w, \"%s\", \"value\") }\n"+
			"func file(w *os.File) { fmt.Fprintf(w, \"%s\", \"value\") }\n")
	if err != nil {
		return nil, err
	}

	matches := 0

	for _, issue := range fallibleFormatter {
		if issue.Linter == "errcheck" && strings.Contains(issue.Text, "fmt.Fprintf") {
			matches++
		}
	}

	if matches != 2 {
		problems = append(problems, "real errcheck did not reject both fallible formatter sinks")
	}

	return problems, nil
}

// whitespaceControls proves both alternatives of the centrally recorded multiline-layout conflict.
func whitespaceControls(ctx context.Context, scratch, binary string) ([]string, error) {
	file := filepath.Join(scratch, scratchLintConfigName)

	original, err := readInRoot(scratch, scratchLintConfigName)
	if err != nil {
		return nil, fmt.Errorf("read control config: %w", err)
	}
	defer restore(file, original)

	strict := strings.ReplaceAll(
		strings.ReplaceAll(string(original), "multi-if: false", "multi-if: true"),
		"multi-func: false",
		"multi-func: true",
	)
	if err = os.WriteFile(file, []byte(strict), fileMode); err != nil {
		return nil, fmt.Errorf("write paired control config: %w", err)
	}

	without := "package lintcontrols\nfunc multi(\n value int,\n other int,\n) int {\n" +
		" if value>other &&\n other>0 {\n return value\n }\n return other\n}\n"

	missing, err := lintSelectedIssues(ctx, scratch, binary, without, "whitespace,revive,wsl_v5")
	if err != nil {
		return nil, err
	}

	with := strings.ReplaceAll(strings.ReplaceAll(without, ") int {\n", ") int {\n\n"), "other>0 {\n", "other>0 {\n\n")

	extra, err := lintSelectedIssues(ctx, scratch, binary, with, "whitespace,revive,wsl_v5")
	if err != nil {
		return nil, err
	}

	var problems []string
	if countLintIssues(missing, "whitespace", "multi-line statement") != 2 {
		problems = append(problems, "multiline condition/signature whitespace controls were not rejected")
	}

	if !hasLintIssue(extra, "wsl_v5", "leading-whitespace") || !hasLintIssue(extra, reviveLinter, "empty-lines") {
		problems = append(problems, "inserted multiline blanks did not demonstrate the incompatible block checks")
	}

	return problems, nil
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

func sourceLimitControls(ctx context.Context, scratch, binary string) ([]string, error) {
	var problems []string

	oversized, err := lintControlIssues(ctx, scratch, binary, oversizedSource())
	if err != nil {
		return nil, err
	}

	for _, rule := range []string{"file-length-limit", "max-public-structs"} {
		if hasLintIssue(oversized, reviveLinter, rule) {
			problems = append(problems, "replaced native algorithm remains active: "+rule)
		}
	}

	limits, scanErr := repopolicy.ScanSourceLimits(ctx, scratch)
	if scanErr != nil {
		return nil, scanErr
	}

	if len(limits) != 2 {
		problems = append(problems, "owned-source scan did not reject both source-limit negative controls")
	}

	var unexported strings.Builder
	unexported.WriteString("// Package lintcontrols exercises lowercase Unicode type names.\npackage lintcontrols\n")

	for index := range 21 {
		fmt.Fprintf(&unexported, "type σhape%d struct{}\n", index)
	}

	positive, positiveErr := lintControlIssues(ctx, scratch, binary, unexported.String())
	if positiveErr != nil {
		return nil, positiveErr
	}

	for _, rule := range []string{"file-length-limit", "max-public-structs"} {
		if hasLintIssue(positive, reviveLinter, rule) {
			problems = append(problems, "native source-limit false positive: "+rule)
		}
	}

	limits, scanErr = repopolicy.ScanSourceLimits(ctx, scratch)
	if scanErr != nil {
		return nil, scanErr
	}

	if len(limits) != 0 {
		problems = append(problems, "lowercase Unicode types rejected by owned-source scan")
	}

	return problems, nil
}
