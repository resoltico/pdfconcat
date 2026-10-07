// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// architecture verifies complete production classification and real native-depguard controls.
func architecture(ctx context.Context, args []string) error {
	if err := newFlags(architectureCommand).Parse(args); err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	problems, err := architectureScope(ctx, root)
	if err != nil {
		return err
	}

	if len(problems) > 0 {
		return report(architectureCommand, problems, "")
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		return err
	}

	if importErr := architectureImports(ctx, root, binary); importErr != nil {
		return importErr
	}

	problems, err = architectureControls(ctx, root, binary)
	if err != nil {
		return err
	}

	return report(
		architectureCommand,
		problems,
		"every production file classified across compiler-selected target scopes; compiled import controls enforced",
	)
}

func architectureScope(ctx context.Context, root string) ([]string, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}

	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		return nil, err
	}

	owners, err := repopolicy.ProductionOwners(config, module)
	if err != nil {
		return nil, err
	}

	files, err := repopolicy.OwnedGoSources(root)
	if err != nil {
		return nil, err
	}

	dirs, err := repopolicy.OwnedGoDirectories(root)
	if err != nil {
		return nil, err
	}

	if len(dirs) == 0 {
		return nil, fmt.Errorf("%w: no owned package selection", errGate)
	}

	patterns := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		patterns = append(patterns, "./"+dir)
	}

	compiled := map[string]bool{}

	for _, target := range archiveTargets() {
		output, loadErr := (&command{
			dir: root, name: goTool, args: append([]string{goListVerb, "-e", jsonFlag}, patterns...), env: architectureTargetEnv(target),
		}).output(ctx)
		if loadErr != nil {
			return nil, fmt.Errorf("architecture discovery for %s: %w", target, loadErr)
		}

		if err = collectArchitectureFiles(output, module, compiled); err != nil {
			return nil, fmt.Errorf("architecture discovery for %s: %w", target, err)
		}
	}

	return repopolicy.ProductionClassificationIssues(owners, files, compiled), nil
}

func collectArchitectureFiles(output, module string, compiled map[string]bool) error {
	decoder := json.NewDecoder(strings.NewReader(output))
	count := 0

	for {
		var pkg discoveredPackage

		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("decode architecture packages: %w", err)
		}

		pattern, err := hostOwnedPattern(&pkg, module)
		if err != nil {
			return err
		}

		if len(pkg.DepsErrors) > 0 {
			return fmt.Errorf("%w: package dependency error: %s", errGate, pkg.DepsErrors[0].Err)
		}

		count++

		if pattern == "" {
			continue
		}

		dir := strings.TrimPrefix(pattern, "./")
		for _, file := range compiledPackageFiles(&pkg) {
			compiled[path.Join(dir, file)] = true
		}
	}

	if count == 0 {
		return fmt.Errorf("%w: empty architecture package discovery", errGate)
	}

	return nil
}

// architectureImports uses the installed native checker to analyze every supported target's selected
// owned packages. This is static cross-target import analysis, not native runtime execution.
func architectureImports(ctx context.Context, root, binary string) error {
	dirs, err := repopolicy.OwnedGoDirectories(root)
	if err != nil {
		return err
	}

	module, err := modulePath(root)
	if err != nil {
		return err
	}

	patterns := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		patterns = append(patterns, "./"+dir)
	}

	if len(patterns) == 0 {
		return fmt.Errorf("%w: no architecture import selections", errGate)
	}

	for _, target := range archiveTargets() {
		output, loadErr := (&command{
			dir: root, name: goTool,
			args: append([]string{goListVerb, "-e", jsonFlag}, patterns...),
			env:  architectureTargetEnv(target),
		}).output(
			ctx,
		)
		if loadErr != nil {
			return fmt.Errorf("discover architecture imports for %s: %w", target, loadErr)
		}

		packages, parseErr := parseOwnedPackages(output, module)
		if parseErr != nil {
			return parseErr
		}

		log.Printf("architecture: static import analysis for %s (%d owned packages)", target, len(packages))

		lintArgs := append([]string{runVerb, serialLintRunners, enableOnlyFlag, dependencyLinter}, packages...)
		checker := &command{
			dir: root, name: binary, args: lintArgs, env: architectureTargetEnv(target),
			stdout: log.Writer(), stderr: log.Writer(),
		}

		runErr := checker.run(ctx)
		if runErr != nil {
			return fmt.Errorf("architecture imports for %s: %w", target, runErr)
		}
	}

	return nil
}

func compiledPackageFiles(pkg *discoveredPackage) []string {
	files := append([]string(nil), pkg.GoFiles...)
	files = append(files, pkg.CgoFiles...)
	files = append(files, pkg.TestGoFiles...)

	return append(files, pkg.XTestGoFiles...)
}
