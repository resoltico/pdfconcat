// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// prepareFuzzCancellationFixture compiles the exact fuzz-instrumented graph without executing it.
// Its real worker lifecycle remains the responsibility of the unchanged runFuzzTarget invocation.
func prepareFuzzCancellationFixture(ctx context.Context, directory string) (string, error) {
	binary := filepath.Join(directory, "cancellation-fuzz.test")

	build := &command{dir: directory, name: goTool, fuzzLifecycle: true, args: []string{
		testVerb, "-c", "-fuzz", "^FuzzHang$", "-o", binary, fuzzFixturePackage,
	}}
	if output, err := build.output(ctx); err != nil {
		return "", fmt.Errorf("prepare instrumented cancellation fixture: %w\n%s", err, output)
	}

	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return "", fmt.Errorf("read instrumented fixture identity: %w", err)
	}

	if info.Path != fuzzFixturePackage+".test" || info.Main.Path != fuzzFixturePackage || info.GoVersion != runtime.Version() {
		return "", fmt.Errorf("%w: instrumented fixture package/compiler identity differs", errGate)
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", fmt.Errorf("open instrumented fixture directory: %w", err)
	}

	content, readErr := root.ReadFile(filepath.Base(binary))
	if err = errors.Join(readErr, root.Close()); err != nil {
		return "", fmt.Errorf("read instrumented fixture: %w", err)
	}

	if contextErr := ctx.Err(); contextErr != nil {
		return "", fmt.Errorf("instrumented fixture preparation canceled: %w", contextErr)
	}

	digest := sha256.Sum256(content)

	return fmt.Sprintf("%s %s SHA256:%x", info.GoVersion, info.Path, digest), nil
}

func TestFuzzFixturePreparationPreservesCanceledCallerAndNoOutput(t *testing.T) {
	t.Parallel()
	directory := fuzzLifecycleFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := prepareFuzzCancellationFixture(ctx, directory); !errors.Is(err, context.Canceled) {
		t.Fatalf("instrumentation preparation lost cancellation: %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, "cancellation-fuzz.test")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled preparation produced an executable: %v", err)
	}
}
