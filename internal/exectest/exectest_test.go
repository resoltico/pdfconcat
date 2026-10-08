// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package exectest_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

const commandPackage = "./cmd/pdfconcat"

func TestMain(m *testing.M) {
	// The test runner exits with m.Run's code after TestMain returns, as of Go 1.15.
	m.Run()
	exectest.Cleanup()
}

// runExecutable builds the real executable and runs it once with no arguments, whatever it answers.
func runExecutable(t *testing.T) {
	t.Helper()

	path := exectest.Build(t, commandPackage)

	err := exectest.Command(t.Context(), path).Run()
	if err == nil {
		return
	}

	exitErr, isExit := errors.AsType[*exec.ExitError](err)
	if !isExit {
		t.Fatal(err)
	}

	t.Logf("the executable exited with status %d", exitErr.ExitCode())
}

func TestBuildCachesOneExecutablePerPackage(t *testing.T) {
	t.Setenv(exectest.EnvCoverDir, "")

	first := exectest.Build(t, commandPackage)
	second := exectest.Build(t, commandPackage)

	if first != second {
		t.Fatalf("built twice: %s and %s", first, second)
	}
}

// TestCoverageDataFromExecutable proves the contract the coverage gate relies on: with the
// environment variables set, a run of the built executable leaves coverage data behind.
func TestCoverageDataFromExecutable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(exectest.EnvCoverDir, dir)
	t.Setenv(exectest.EnvCoverPkg, "github.com/resoltico/pdfconcat/...")

	runExecutable(t)

	files, err := filepath.Glob(filepath.Join(dir, "cov*"))

	const wantFiles = 2 // One covmeta and one covcounters file.
	if err != nil || len(files) < wantFiles {
		t.Fatalf("expected covmeta and covcounters files in %s, found %v (%v)", dir, files, err)
	}
}

func TestPlainBuildWithoutCoverage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(exectest.EnvCoverDir, "")

	runExecutable(t)

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("a plain run must not write coverage data: %v (%v)", files, err)
	}
}
