// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	selectedLintAdapter   = "selected"
	unexcludedLintAdapter = "unexcluded"
	positionLintAdapter   = "positions"
	lintCacheEnvironment  = "GOLANGCI_LINT_CACHE"
)

func TestMissingLintReportPreservesRealCacheInitializationFailure(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		t.Fatal(err)
	}

	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(lintCacheEnvironment, filepath.Join(t.TempDir(), "blocked"))

	cases := []string{selectedLintAdapter, unexcludedLintAdapter, architectureCommand, positionLintAdapter}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			scratch := t.TempDir()
			writeFailureLintFixture(t, scratch, config)

			blocked := filepath.Join(scratch, "cache")
			if writeErr := os.WriteFile(blocked, []byte("not a directory"), fileMode); writeErr != nil {
				t.Fatal(writeErr)
			}

			t.Setenv(lintCacheEnvironment, blocked)

			failure := failingLintAdapter(t, scratch, binary, name)

			var exited *exec.ExitError
			if !errors.Is(failure, os.ErrNotExist) || !errors.As(failure, &exited) || exited.ExitCode() != exitIssuesFound ||
				!strings.Contains(failure.Error(), "failed to initialize build cache") || !strings.Contains(failure.Error(), blocked) {
				t.Fatalf("native cache failure hidden by missing JSON: %v", failure)
			}
		})
	}
}

func TestFailedLintRunCannotReusePreviousRealReport(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	binary, err := findTool(root, lintTool)
	if err != nil {
		t.Fatal(err)
	}

	config, err := readInRoot(root, lintConfigFileName)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(lintCacheEnvironment, filepath.Join(t.TempDir(), "outer-cache"))

	for _, name := range []string{selectedLintAdapter, unexcludedLintAdapter, architectureCommand, positionLintAdapter} {
		t.Run(name, func(t *testing.T) {
			scratch := t.TempDir()
			writeFailureLintFixture(t, scratch, config)
			seedRealLintReport(t, scratch, binary)

			blocked := filepath.Join(scratch, "cache")
			if writeErr := os.WriteFile(blocked, []byte("not a directory"), fileMode); writeErr != nil {
				t.Fatal(writeErr)
			}

			t.Setenv(lintCacheEnvironment, blocked)

			failure := failingLintAdapter(t, scratch, binary, name)
			if failure == nil || !strings.Contains(failure.Error(), "failed to initialize build cache") {
				t.Fatalf("failed native run consumed a previous report: %v", failure)
			}
		})
	}
}

func seedRealLintReport(t *testing.T, root, binary string) {
	t.Helper()

	output, err := (&command{
		dir: root, name: binary,
		args: []string{
			runVerb, serialLintRunners, lintNoFixFlag, configFlag, filepath.Join(root, lintConfigFileName),
			enableOnlyFlag, "unused", "--output.json.path=" + filepath.Join(root, lintReportFileName), allPackages,
		},
		env: []string{"GOLANGCI_LINT_CACHE=" + filepath.Join(root, "valid-cache")},
	}).output(t.Context())
	if err != nil {
		t.Fatalf("real positive report prerequisite failed: %v %s", err, output)
	}

	if _, readErr := readInRoot(root, lintReportFileName); readErr != nil {
		t.Fatal(readErr)
	}
}

func failingLintAdapter(t *testing.T, scratch, binary, name string) error {
	t.Helper()

	switch name {
	case selectedLintAdapter:
		_, err := lintSelectedIssues(t.Context(), scratch, binary, "package fixture\n", "unused")
		return err
	case unexcludedLintAdapter:
		_, err := runUnexcludedLint(t.Context(), scratch, binary,
			filepath.Join(scratch, scratchLintConfigName), filepath.Join(scratch, lintReportFileName), scratch)

		return err
	case architectureCommand:
		_, runErr, err := architectureControlIssues(t.Context(), scratch, binary, "")
		if runErr == nil {
			t.Fatal("blocked cache unexpectedly allowed architecture checker execution")
		}

		return err
	case positionLintAdapter:
		_, runErr, err := positionControlIssues(t.Context(), scratch, binary)
		if runErr == nil {
			t.Fatal("blocked cache unexpectedly allowed position checker execution")
		}

		return err
	default:
		t.Fatalf("unknown lint adapter %s", name)
		return nil
	}
}

func writeFailureLintFixture(t *testing.T, root string, config []byte) {
	t.Helper()

	files := map[string][]byte{
		moduleFileName:        []byte("module fixture\n\ngo 1.27.1\n"),
		"fixture.go":          []byte("package fixture\n"),
		lintConfigFileName:    config,
		scratchLintConfigName: config,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, fileMode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPresentLintReportKeepsExpectedIssueExitSeparate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	content := []byte(`{"Issues":[]}`)
	if err := os.WriteFile(filepath.Join(root, lintReportFileName), content, fileMode); err != nil {
		t.Fatal(err)
	}

	data, err := readLintRunReport(root, &exec.ExitError{}, "ordinary diagnostics")
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("present report became an infrastructure error: %q %v", data, err)
	}
}

func TestMissingLintReportWithoutChildErrorRetainsProtocolEvidence(t *testing.T) {
	t.Parallel()

	_, err := readLintRunReport(t.TempDir(), nil, "child completed without required report")

	var exited *exec.ExitError
	if !errors.Is(err, os.ErrNotExist) || errors.As(err, &exited) ||
		!strings.Contains(err.Error(), "child completed without required report") {
		t.Fatalf("missing-report protocol failure lost: %v", err)
	}
}

func TestLintReportPreparationStopsAtNonemptyDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	directory := filepath.Join(root, lintReportFileName)
	if err := os.Mkdir(directory, dirMode); err != nil {
		t.Fatal(err)
	}

	child := filepath.Join(directory, "retained")
	if err := os.WriteFile(child, []byte("retained evidence"), fileMode); err != nil {
		t.Fatal(err)
	}

	if err := prepareLintRunReport(root); err == nil {
		t.Fatal("nonempty directory treated as a removable old report")
	}

	data, err := readInRoot(directory, "retained")
	if err != nil || string(data) != "retained evidence" {
		t.Fatalf("report preparation removed unrelated contents: %q %v", data, err)
	}
}
