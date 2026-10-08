// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

type (
	discoveredPackage struct {
		DepsErrors      []discoveryError `json:"depserrors"`
		Error           *discoveryError  `json:"error"`
		ImportPath      string           `json:"importpath"`
		Dir             string           `json:"dir"`
		GoFiles         []string         `json:"gofiles"`
		CgoFiles        []string         `json:"cgofiles"`
		TestGoFiles     []string         `json:"testgofiles"`
		XTestGoFiles    []string         `json:"xtestgofiles"`
		IgnoredGoFiles  []string         `json:"ignoredgofiles"`
		EmbedFiles      []string         `json:"embedfiles"`
		TestEmbedFiles  []string         `json:"testembedfiles"`
		XTestEmbedFiles []string         `json:"xtestembedfiles"`
	}

	discoveryError struct {
		Err string `json:"err"`
	}
)

// ownedPackages adds explicitly compiled fixture and hidden packages to Go's normal package scope.
func ownedPackages(ctx context.Context, root string) ([]string, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}

	dirs, err := repopolicy.OwnedGoDirectories(root)
	if err != nil {
		return nil, err
	}

	patterns := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		patterns = append(patterns, "./"+dir)
	}

	if len(patterns) == 0 {
		return nil, fmt.Errorf("%w: no owned Go package directories", errGate)
	}

	output, err := goCommand(root, append([]string{goListVerb, "-e", jsonFlag}, patterns...)...).output(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover owned packages: %w", err)
	}

	return parseOwnedPackages(output, module)
}

func parseOwnedPackages(output, module string) ([]string, error) {
	decoder := json.NewDecoder(strings.NewReader(output))

	var packages []string

	for {
		var pkg discoveredPackage

		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("decode owned package discovery: %w", err)
		}

		pattern, patternErr := hostOwnedPattern(&pkg, module)
		if patternErr != nil {
			return nil, patternErr
		}

		if pattern != "" {
			packages = append(packages, pattern)
		}
	}

	if len(packages) == 0 {
		return nil, fmt.Errorf("%w: no host-owned packages discovered", errGate)
	}

	return packages, nil
}

func lintOwned(ctx context.Context, args []string) error {
	if err := newFlags(lintCommand).Parse(args); err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	if _, versionErr := configuredProjectVersion(root); versionErr != nil {
		return versionErr
	}

	if limitErr := sourceLimits(root); limitErr != nil {
		return limitErr
	}

	packages, err := ownedPackages(ctx, root)
	if err != nil {
		return err
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		return err
	}

	log.Printf("lint: %d owned compiled packages, including fixtures", len(packages))

	err = (&command{
		dir: root, name: binary,
		args: append([]string{
			runVerb, serialLintRunners, lintNoFixFlag, configFlag, filepath.Join(root, lintConfigFileName),
		}, packages...),
		stdout: log.Writer(), stderr: log.Writer(),
	}).run(ctx)
	if err != nil {
		return fmt.Errorf("configured owned lint: %w", err)
	}

	return nil
}

func formatOwned(ctx context.Context, args []string) error {
	if err := newFlags(formatCommand).Parse(args); err != nil {
		return fmt.Errorf(parseFlagsError, err)
	}

	root, err := repoRoot()
	if err != nil {
		return err
	}

	dirs, err := repopolicy.OwnedGoDirectories(root)
	if err != nil {
		return err
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		return err
	}

	output, err := (&command{
		dir: root, name: binary,
		args: append([]string{"fmt", "--diff", configFlag, filepath.Join(root, lintConfigFileName)}, dirs...),
	}).output(ctx)
	if err != nil {
		return fmt.Errorf("owned formatting: %w\n%s", err, output)
	}

	if strings.TrimSpace(output) != "" {
		return fmt.Errorf("%w: formatting differences in owned Go sources:\n%s", errGate, output)
	}

	return nil
}

func hostOwnedPattern(pkg *discoveredPackage, module string) (string, error) {
	if len(pkg.DepsErrors) > 0 {
		return "", fmt.Errorf("%w: package dependency error: %s", errGate, pkg.DepsErrors[0].Err)
	}

	if pkg.Error != nil {
		taggedOnly := len(pkg.IgnoredGoFiles) > 0 && len(pkg.GoFiles) == 0 && len(pkg.CgoFiles) == 0
		if taggedOnly && strings.Contains(pkg.Error.Err, "build constraints exclude all Go files") {
			return "", nil
		}

		return "", fmt.Errorf("%w: discover %s: %s", errGate, pkg.ImportPath, pkg.Error.Err)
	}

	if pkg.ImportPath == module {
		return ".", nil
	}

	if suffix, found := strings.CutPrefix(pkg.ImportPath, module+"/"); found {
		return "./" + suffix, nil
	}

	return "", fmt.Errorf("%w: owned directory resolved outside module: %s", errGate, pkg.ImportPath)
}

func sourceLimits(root string) error {
	problems, err := repopolicy.ScanSourceLimits(root)
	if err != nil {
		return err
	}

	return report("source-limits", problems, "every owned physical source file satisfies size and exported-type limits")
}
