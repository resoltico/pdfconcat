//go:build unix

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package capture_test

import (
	"errors"
	"os"
	"testing"

	"github.com/resoltico/pdfconcat/internal/capture"
	"github.com/resoltico/pdfconcat/internal/exectest"
	"github.com/resoltico/pdfconcat/internal/permissiontest"
)

const registryCWDHelper = "PDFCONCAT_REGISTRY_CWD_HELPER"

func TestRegistryRelativePathsRejectInaccessibleWorkingDirectory(t *testing.T) {
	if os.Getenv(registryCWDHelper) != "" {
		checkRegistryInaccessibleCWD(t)
		return
	}

	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	command := exectest.Command(t.Context(), executable, "-test.run=^TestRegistryRelativePathsRejectInaccessibleWorkingDirectory$")

	command.Env = append(command.Env, registryCWDHelper+"=1")
	if coverDir := os.Getenv(exectest.EnvCoverDir); coverDir != "" {
		command.Args = append(command.Args, "-test.gocoverdir="+coverDir)
	}

	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("actual unavailable cwd control failed: %v\n%s", runErr, output)
	}
}

func checkRegistryInaccessibleCWD(t *testing.T) {
	t.Helper()
	permissiontest.RequireEnforcement(t)
	dir := t.TempDir()

	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatal(statErr)
	}

	t.Chdir(dir)

	if chmodErr := os.Chmod(dir, 0); chmodErr != nil {
		t.Fatal(chmodErr)
	}

	defer func() {
		if restoreErr := os.Chmod(dir, info.Mode().Perm()); restoreErr != nil {
			t.Error(restoreErr)
		}
	}()

	if _, err := os.Getwd(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("native cwd denial not enforced: %v", err)
	}

	registry := capture.NewRegistry()
	if _, err := registry.Add(capture.RoleOutput, "relative.pdf"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("relative artifact did not retain cwd failure: %v", err)
	}

	if err := registry.RetireReport("relative.json"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("relative retirement did not retain cwd failure: %v", err)
	}
}
