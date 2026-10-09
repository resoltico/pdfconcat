// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const expiredCollectorChild = "expired"

func TestOwnedCollectorSetupFailureReleasesCompiledDirectory(t *testing.T) {
	t.Parallel()

	parent := t
	for _, scenario := range []string{"unlaunchable", expiredCollectorChild} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			directory := failedCollectorSetupDirectory(t, scenario)
			// Parent cleanup follows every subtest cleanup, including the real sampler release.
			parent.Cleanup(func() {
				if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
					parent.Errorf("failed launch leaked compiled collector directory: %s %v", directory, err)
				}
			})
		})
	}
}

func failedCollectorSetupDirectory(t *testing.T, scenario string) string {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	executable := filepath.Join(t.TempDir(), "missing-child")

	if scenario == expiredCollectorChild {
		cancel()

		executable = successfulChildExecutable
	}

	fixture, err := startOwnedCollectorProcess(t, exec.CommandContext(ctx, executable))
	if err == nil || fixture == nil || fixture.command.Process != nil {
		t.Fatalf("real child setup failure not reached: %+v %v", fixture, err)
	}

	if scenario == expiredCollectorChild && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	if scenario == "unlaunchable" && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	directory := fixture.sampler.directory
	if directory == "" {
		t.Fatal("native compiled directory was not acquired before launch")
	}

	if _, statErr := os.Stat(directory); statErr != nil {
		t.Fatal(statErr)
	}

	return directory
}
