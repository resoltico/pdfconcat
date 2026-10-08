// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPositionLintUsesOwnedConfigDespiteAlternateExtension(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		t.Fatal(err)
	}

	scratch := t.TempDir()
	if err = prepareLintControls(root, scratch); err != nil {
		t.Fatal(err)
	}

	if err = os.Rename(filepath.Join(scratch, scratchLintConfigName), filepath.Join(scratch, lintConfigFileName)); err != nil {
		t.Fatal(err)
	}

	source := "package capture\nimport \"os\"\nfunc Read(name string)([]byte,error){return os.ReadFile(name)}\n"
	if err = writeArchitectureSource(scratch, positionControlFile, source); err != nil {
		t.Fatal(err)
	}

	alternate := "version: \"2\"\nlinters:\n  default: none\n  settings:\n    gosec:\n      excludes: [G304]\n"
	if err = os.WriteFile(filepath.Join(scratch, ".golangci.yaml"), []byte(alternate), fileMode); err != nil {
		t.Fatal(err)
	}

	issues, runErr, err := positionControlIssues(t.Context(), scratch, binary)
	if err != nil {
		t.Fatal(err)
	}

	if runErr == nil || !hasLintIssue(issues, securityLinter, "G304") {
		t.Fatal("alternate configuration suppressed the owned security control")
	}
}

func TestSelectedLintCannotHonorConfiguredAutofix(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		t.Fatal(err)
	}

	scratch := t.TempDir()
	if err = prepareLintControls(root, scratch); err != nil {
		t.Fatal(err)
	}

	config, err := readInRoot(scratch, scratchLintConfigName)
	if err != nil {
		t.Fatal(err)
	}

	configuredFix := strings.Replace(string(config), "issues:\n", "issues:\n  fix: true\n", 1)

	if err = os.WriteFile(filepath.Join(scratch, scratchLintConfigName), []byte(configuredFix), fileMode); err != nil {
		t.Fatal(err)
	}

	source := "package lintcontrols\n\nfunc Value(ready bool) int {\n\tif ready {\n\t\treturn 1\n\t}\n\treturn 2\n}\n"

	issues, err := lintSelectedIssues(t.Context(), scratch, binary, source, "wsl_v5")
	if err != nil {
		t.Fatal(err)
	}

	actual, err := readInRoot(scratch, "fixture.go")
	if err != nil {
		t.Fatal(err)
	}

	if len(issues) == 0 || string(actual) != source {
		t.Fatalf("configured autofix repaired the lint input: issues=%v source=%q", issues, actual)
	}
}
