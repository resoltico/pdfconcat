// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis
package main

import (
	"bytes"
	"context"
	"debug/buildinfo"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	exitIssuesFound      = 1
	toolVersionsFileName = "tools/versions.env"
)

func lintConfig(ctx context.Context, args []string) error {
	if err := newFlags(lintConfigCommand).Parse(args); err != nil {
		return err
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if err := sourceLimits(ctx, root); err != nil {
		return err
	}
	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return err
	}
	problems, err := repopolicy.CorrectnessProfileIssues(config)
	if err != nil {
		return err
	}
	binary, err := findTool(root, lintTool)
	if err != nil {
		return err
	}
	extra, err := checkBinary(ctx, root, binary, nil)
	if err != nil {
		return err
	}
	return report(lintConfigCommand, append(problems, extra...), "explicit stock correctness profile and compiled negative controls")
}

func checkBinary(ctx context.Context, root, binary string, _ *repopolicy.Registry) ([]string, error) {
	versions, err := readToolVersions(root)
	if err != nil {
		return nil, err
	}
	if err := checkLinterIdentity(ctx, root, binary, versions); err != nil {
		return nil, err
	}
	if _, err := (&command{dir: root, name: binary, args: []string{"config", "verify", configFlag, filepath.Join(root, lintConfigFileName)}}).output(ctx); err != nil {
		return nil, err
	}
	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return nil, err
	}
	problems, err := repopolicy.CorrectnessProfileIssues(config)
	if err != nil {
		return nil, err
	}
	sdk, err := goCommand(root, goEnvVerb, "GOROOT").output(ctx)
	if err != nil {
		return nil, err
	}
	problems = append(problems, repopolicy.InfallibleMethodPremiseIssues(repopolicy.InfallibleWriterFunctions(), fileReader(strings.TrimSpace(sdk)))...)
	controls, err := lintBoundaryControls(ctx, root, binary)
	if err != nil {
		return nil, err
	}
	positions, err := lintPositionControls(ctx, root, binary)
	if err != nil {
		return nil, err
	}
	return append(append(problems, controls...), positions...), nil
}

func checkLinterIdentity(_ context.Context, _ string, binary string, versions map[string]string) error {
	if err := repopolicy.VerifyGolangciBinaryMetadata(binary); err != nil {
		return err
	}
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return err
	}
	if info.Main.Version != versions["GOLANGCI_LINT_VERSION"] || info.Main.Replace != nil {
		return fmt.Errorf("%w: linter module differs from pinned stock version", errGate)
	}
	return nil
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

func runLintReport(ctx context.Context, root, binary, configFile, reportFile, scratch string) ([]byte, error) {
	if err := prepareLintRunReport(scratch); err != nil {
		return nil, err
	}

	var stdout bytes.Buffer

	var env []string
	if runtime.GOOS == nativeProgressOS {
		env = nativeProgressEnv()
	}

	packages, err := ownedPackagesWithEnv(ctx, root, env)
	if err != nil {
		return nil, err
	}

	run := &command{
		dir:    root,
		name:   binary,
		stdout: &stdout,
		stderr: &stdout,
		env:    env,
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
