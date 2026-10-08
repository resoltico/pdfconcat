// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// exitIssuesFound is golangci-lint's exit status when it ran and reported issues.
const (
	exitIssuesFound      = 1
	toolVersionsFileName = "tools/versions.env"
)

// lintConfig verifies .golangci.yml against the registry and the pinned golangci-lint binary.
func lintConfig(ctx context.Context, args []string) error {
	set := newFlags(lintConfigCommand)
	printRules := set.Bool("print-rules", false, "print the exclusion rules .golangci.yml must carry, then exit")

	err := set.Parse(args)
	if err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	if limitErr := sourceLimits(root); limitErr != nil {
		return limitErr
	}

	registry, err := repopolicy.LoadRegistry(filepath.Join(root, registryName))
	if err != nil {
		return fmt.Errorf(loadRegistryError, err)
	}

	if *printRules {
		return printExclusionRules(registry)
	}

	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return err
	}

	problems := repopolicy.RepositoryIssues(registry.Exceptions, fileReader(root))

	configProblems, err := repopolicy.LintConfigIssues(registry.Exceptions, config)
	if err != nil {
		return fmt.Errorf("compare configuration: %w", err)
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		return err
	}

	binaryProblems, err := checkBinary(ctx, root, binary, registry)
	if err != nil {
		return err
	}

	problems = append(problems, configProblems...)

	return report(
		lintConfigCommand,
		append(problems, binaryProblems...),
		fmt.Sprintf("%d lint exceptions match", len(registry.For(repopolicy.ToolLint))),
	)
}

// checkBinary checks the pinned version, `config verify`, and the premise of each exception.
func checkBinary(ctx context.Context, root, binary string, registry *repopolicy.Registry) ([]string, error) {
	var problems []string

	versions, err := readToolVersions(root)
	if err != nil {
		return nil, err
	}

	if identityErr := checkLinterIdentity(ctx, root, binary, versions); identityErr != nil {
		return nil, identityErr
	}

	_, err = (&command{
		dir: root, name: binary,
		args: []string{"config", "verify", configFlag, filepath.Join(root, lintConfigFileName)},
	}).output(ctx)
	if err != nil {
		problems = append(problems, "golangci-lint config verify failed: "+err.Error())
	}

	listing, err := (&command{
		dir: root, name: binary,
		args: []string{"linters", "--json", configFlag, filepath.Join(root, lintConfigFileName)},
	}).output(ctx)
	if err != nil {
		return nil, fmt.Errorf("golangci-lint linters: %w", err)
	}

	known, err := repopolicy.ParseLinterInfo([]byte(listing))
	if err != nil {
		return nil, fmt.Errorf("parse linter list: %w", err)
	}

	problems = append(problems, repopolicy.PremiseIssues(registry.Exceptions, known)...)

	sdk, err := goCommand(root, "env", "GOROOT").output(ctx)
	if err != nil {
		return nil, fmt.Errorf("find selected Go SDK: %w", err)
	}

	problems = append(problems, repopolicy.InfallibleMethodPremiseIssues(registry.Exceptions, fileReader(strings.TrimSpace(sdk)))...)

	controls, err := lintBoundaryControls(ctx, root, binary)
	if err != nil {
		return nil, err
	}

	positionProblems, positionErr := lintPositionControls(ctx, root, binary)
	if positionErr != nil {
		return nil, positionErr
	}

	return append(append(problems, controls...), positionProblems...), nil
}

// checkLinterIdentity verifies the selected command's native source variant before running controls.
func checkLinterIdentity(ctx context.Context, root, binary string, versions map[string]string) error {
	if metadataErr := repopolicy.VerifyGolangciBinaryMetadata(binary); metadataErr != nil {
		return metadataErr
	}

	identity, identityErr := repopolicy.GolangciBuildIdentity(versions)
	if identityErr != nil {
		return identityErr
	}

	patch, patchErr := readInRoot(root, "tools/lint-patches/golangci-lint-physical-source.patch")
	if patchErr != nil {
		return patchErr
	}

	if verifyErr := repopolicy.VerifyGolangciPatch(patch, versions); verifyErr != nil {
		return verifyErr
	}

	version, err := (&command{dir: root, name: binary, args: []string{versionVerb, "--json"}}).output(ctx)
	if err != nil {
		return fmt.Errorf("golangci-lint version: %w", err)
	}

	return repopolicy.VerifyGolangciReportedIdentity([]byte(version), identity)
}

func readToolVersions(root string) (map[string]string, error) {
	content, err := readInRoot(root, toolVersionsFileName)
	if err != nil {
		return nil, err
	}

	versions, err := repopolicy.ParseToolVersions(string(content))
	if err != nil {
		return nil, fmt.Errorf("parse tools/versions.env: %w", err)
	}

	return versions, nil
}

// lintStale runs golangci-lint with the diagnostic exclusion rules removed and fails when a registry
// exclusion matches none of the diagnostics that run reports. The run itself is expected to report
// the excluded diagnostics, so its own exit status is not a failure.
func lintStale(ctx context.Context, args []string) error {
	err := newFlags(lintStaleCommand).Parse(args)
	if err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	if limitErr := sourceLimits(root); limitErr != nil {
		return limitErr
	}

	registry, err := repopolicy.LoadRegistry(filepath.Join(root, registryName))
	if err != nil {
		return fmt.Errorf(loadRegistryError, err)
	}

	applicable, selectionErr := applicableDiagnosticEntries(ctx, root, registry.Exceptions)
	if selectionErr != nil {
		return selectionErr
	}

	data, err := unexcludedReport(ctx, root)
	if err != nil {
		return err
	}

	issues, err := repopolicy.ParseIssues(data, filepath.ToSlash(root))
	if err != nil {
		return fmt.Errorf("parse lint report: %w", err)
	}

	for _, issue := range issues {
		if issue.Linter == "typecheck" {
			return fmt.Errorf("%w: the tree does not type-check, so no linter ran and staleness cannot be judged: %s: %s",
				errGate, issue.File, issue.Text)
		}
	}

	stale := repopolicy.StaleDiagnosticEntries(applicable, issues, runtime.GOOS)
	problems := make([]string, 0, len(stale))

	for _, entry := range stale {
		problems = append(problems, fmt.Sprintf("%s: no unexcluded %s diagnostic in %s matches %q; delete the exclusion",
			entry.ID, entry.Linter, entry.Path, entry.Message))
	}

	return report(
		lintStaleCommand,
		problems,
		fmt.Sprintf("every applicable native-compiled diagnostic exclusion matches (%d diagnostics seen on %s)", len(issues), runtime.GOOS),
	)
}

// unexcludedReport runs golangci-lint with the configuration's exclusion rules removed and returns its JSON report.
func unexcludedReport(ctx context.Context, root string) ([]byte, error) {
	binary, err := findTool(root, lintTool)
	if err != nil {
		return nil, err
	}

	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return nil, err
	}

	stripped, err := repopolicy.ConfigWithoutRules(config)
	if err != nil {
		return nil, fmt.Errorf("remove exclusion rules: %w", err)
	}

	scratch, err := os.MkdirTemp("", "qualitygate-lint-")
	if err != nil {
		return nil, fmt.Errorf("create scratch directory: %w", err)
	}

	defer removeAll(scratch)

	configFile := filepath.Join(scratch, scratchLintConfigName)
	reportFile := filepath.Join(scratch, lintReportFileName)

	err = os.WriteFile(configFile, stripped, fileMode)
	if err != nil {
		return nil, fmt.Errorf("write scratch configuration: %w", err)
	}

	return runUnexcludedLint(ctx, root, binary, configFile, reportFile, scratch)
}

func runUnexcludedLint(ctx context.Context, root, binary, configFile, reportFile, scratch string) ([]byte, error) {
	if err := prepareLintRunReport(scratch); err != nil {
		return nil, err
	}

	var stdout bytes.Buffer

	packages, err := ownedPackages(ctx, root)
	if err != nil {
		return nil, err
	}

	run := &command{
		dir:    root,
		name:   binary,
		stdout: &stdout,
		stderr: &stdout,
		args: append([]string{
			runVerb,
			serialLintRunners,
			lintNoFixFlag,
			configFlag,
			configFile,
			"--path-mode=abs",
			"--output.json.path=" + reportFile,
			"--output.text.path=stdout",
		}, packages...),
	}

	runErr := run.run(ctx)
	if runErr != nil && exitCode(runErr) != exitIssuesFound {
		return nil, fmt.Errorf("golangci-lint run failed: %w\n%s", runErr, indent(stdout.String()))
	}

	return readLintRunReport(scratch, runErr, stdout.String())
}

// exitCode returns the exit status of a failed child process, or -1.
func exitCode(err error) int {
	exitErr, isExit := errors.AsType[*exec.ExitError](err)
	if !isExit {
		return -1
	}

	return exitErr.ExitCode()
}

// report prints the outcome of a gate and returns an error when it has problems.
func report(gate string, problems []string, success string) error {
	if len(problems) == 0 {
		log.Printf("%s: ok: %s", gate, success)

		return nil
	}

	for _, problem := range problems {
		log.Printf("%s: %s", gate, problem)
	}

	return fmt.Errorf("%w: %s failed with %d problem(s)", errGate, gate, len(problems))
}

// printExclusionRules writes the rules block derived from the registry to the log stream.
func printExclusionRules(registry *repopolicy.Registry) error {
	text, err := repopolicy.ExclusionRulesYAML(registry.Exceptions)
	if err != nil {
		return fmt.Errorf("render rules: %w", err)
	}

	log.Print(text)

	return nil
}
