// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package exectest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func commandCoverageDirectory(environment []string) string {
	directory := ""
	for _, value := range environment {
		if strings.HasPrefix(value, "GOCOVERDIR=") {
			directory = strings.TrimPrefix(value, "GOCOVERDIR=")
		}
	}
	return directory
}

func TestConcurrentCoveredExecutablesHaveExclusiveMetadataOwnership(t *testing.T) {
	root := t.TempDir()
	t.Setenv(exectest.EnvCoverDir, root)
	path := exectest.Build(t, commandPackage)
	directories := map[string]bool{}
	var children sync.WaitGroup
	errorsFound := make(chan error, 16)
	for range 16 {
		command := exectest.CoveredCommand(t, t.Context(), path, "version")
		directory := commandCoverageDirectory(command.Env)
		if directory == root || filepath.Dir(directory) != root || directories[directory] {
			t.Fatal("covered children share metadata ownership")
		}
		directories[directory] = true
		children.Add(1)
		go func() { defer children.Done(); _, err := command.CombinedOutput(); errorsFound <- err }()
	}
	children.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	for directory := range directories {
		for _, prefix := range []string{"covmeta.", "covcounters."} {
			files, err := filepath.Glob(filepath.Join(directory, prefix+"*"))
			if err != nil || len(files) == 0 {
				t.Fatalf("completed child lost %s: %v", prefix, err)
			}
		}
	}
	if shared, err := filepath.Glob(filepath.Join(root, "cov*")); err != nil || len(shared) != 0 {
		t.Fatal("metadata escaped child ownership")
	}
}

func TestCoveragePreparationFailurePreventsExecutableLaunch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(exectest.EnvCoverDir, root)
	path := exectest.Build(t, commandPackage)
	command := exectest.CoveredCommand(t, t.Context(), path, "version")
	_, err := command.CombinedOutput()
	var native *os.PathError
	if err == nil || !errors.As(err, &native) || command.Process != nil {
		t.Fatalf("preparation failure launched or lost native cause: %v", err)
	}
}

func TestCancelledLaunchDoesNotReserveEmptyCoverageInput(t *testing.T) {
	root := t.TempDir()
	t.Setenv(exectest.EnvCoverDir, root)
	path := exectest.Build(t, commandPackage)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	command := exectest.CoveredCommand(t, ctx, path, "version")
	if err := command.Run(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled launch left an empty completed-child input")
	}
}

func TestPreparedCoveredCommandReleasesOnlyUnstartedReservation(t *testing.T) {
	root := t.TempDir()
	t.Setenv(exectest.EnvCoverDir, root)
	path := exectest.Build(t, commandPackage)
	var directory string
	t.Run("prepare-only", func(owner *testing.T) {
		command := exectest.CoveredCommand(owner, owner.Context(), path, "version")
		directory = commandCoverageDirectory(command.Env)
		if info, err := os.Stat(directory); err != nil || !info.IsDir() {
			t.Fatal("prepared invocation has no owned directory")
		}
	})
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unstarted reservation survived owner cleanup: %v", err)
	}
}
