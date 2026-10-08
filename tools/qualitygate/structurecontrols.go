// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type functionBoundary struct {
	name, diagnostic  string
	statements, lines int
}

const (
	functionControlFile = "internal/assembly/function_control.go"
	functionControlLine = 4
)

// architectureStructureControls compiles accepted and rejected function boundaries for each supported target.
func architectureStructureControls(ctx context.Context, root, binary string) ([]string, error) {
	defer removeAll(filepath.Join(root, functionControlFile))

	var problems []string

	for _, target := range archiveTargets() {
		for _, boundary := range []functionBoundary{
			{"statements", "too many statements", 40, 0}, {"lines", "too long", 2, 60},
		} {
			issues, err := architectureFunctionBoundary(ctx, root, binary, target, boundary)
			if err != nil {
				return nil, err
			}

			problems = append(problems, issues...)
		}
	}

	return problems, nil
}

func architectureFunctionBoundary(ctx context.Context, root, binary, target string, boundary functionBoundary) ([]string, error) {
	var problems []string

	for _, overflow := range []int{0, 1} {
		if err := writeArchitectureSource(root, functionControlFile, functionBoundarySource(target, boundary, overflow)); err != nil {
			return nil, err
		}

		if err := compileArchitectureControl(ctx, root, target); err != nil {
			return nil, err
		}

		issues, runErr, err := architectureAnalyzerIssues(ctx, root, binary, target, "funlen")
		if err != nil {
			return nil, err
		}

		rejected := runErr != nil &&
			hasPhysicalIssue(issues, functionControlFile, "funlen", boundary.diagnostic, "func bounded()", functionControlLine)
		if overflow == 0 && runErr != nil {
			return nil, fmt.Errorf("%w: %s accepted function control produced diagnostics: %v", errGate, target, issues)
		}

		if (overflow == 1) != rejected {
			problems = append(problems, fmt.Sprintf("%s function %s boundary %d rejected=%t", target, boundary.name, overflow, rejected))
		}
	}

	return problems, nil
}

func functionBoundarySource(target string, boundary functionBoundary, overflow int) string {
	statements, lines := boundary.statements, boundary.lines
	if lines == 0 {
		statements += overflow
	} else {
		lines += overflow
	}

	goos, arch, _ := strings.Cut(target, "/")
	prefix := "//go:build " + goos + " && " + arch + "\n\npackage assembly\nfunc bounded() {\n"
	// A declaration and the final discard are statements; the remaining statements are increments.
	body := "value := 0\n" + strings.Repeat("value++\n", statements-2) + "_ = value\n"
	if lines > statements {
		// Blank lines increase physical length without increasing statement count.
		body += strings.Repeat("\n", lines-statements)
	}

	return prefix + body + "}\n"
}
