// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type sourceDependencyFiles struct{ module, sums []byte }

func snapshotSourceDependencies(sourceDir string) (sourceDependencyFiles, error) {
	module, moduleErr := readFile(sourceDir, sourceModuleFile)
	if moduleErr != nil {
		return sourceDependencyFiles{}, moduleErr
	}

	sums, sumsErr := readFile(sourceDir, "go.sum")
	if sumsErr != nil {
		return sourceDependencyFiles{}, sumsErr
	}

	return sourceDependencyFiles{module: module, sums: sums}, nil
}

func (d sourceDependencyFiles) verify(sourceDir string) error {
	actual, err := snapshotSourceDependencies(sourceDir)
	if err != nil {
		return err
	}

	if !bytes.Equal(d.module, actual.module) || !bytes.Equal(d.sums, actual.sums) {
		return fmt.Errorf("%w: upstream tool go.mod or go.sum changed", errInstall)
	}

	return nil
}

func applySourcePatch(ctx context.Context, sourceDir string, patch []byte, expectedSHA string) error {
	digest := sha256.Sum256(patch)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return fmt.Errorf("%w: source patch digest differs from its pin", errInstall)
	}

	command := exec.CommandContext(ctx, "git", "apply", "--no-index", "--whitespace=error-all", "-")
	command.Dir = sourceDir
	command.Stdin = bytes.NewReader(patch)
	command.Stdout = log.Writer()

	command.Stderr = log.Writer()
	if err := command.Run(); err != nil {
		return fmt.Errorf("apply reviewed source patch: %w", err)
	}

	return nil
}

func runSourceGo(ctx context.Context, sourceDir string, args ...string) error {
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = sourceDir

	command.Env = append(os.Environ(), sourceBuildEnv()...)
	command.Stdout = log.Writer()

	command.Stderr = log.Writer()
	if err := command.Run(); err != nil {
		return fmt.Errorf("upstream tool go %s: %w", strings.Join(args, " "), err)
	}

	return nil
}

// installSourceExecutable replaces the owned development binary only after a complete build and validation.
func installSourceExecutable(binDir, name string, content []byte) error {
	file, createErr := os.CreateTemp(binDir, "."+name+"-*")
	if createErr != nil {
		return fmt.Errorf("stage source-built executable: %w", createErr)
	}

	path := file.Name()
	_, writeErr := file.Write(content)
	modeErr := file.Chmod(execMode)

	closeErr := file.Close()
	if writeErr != nil || modeErr != nil || closeErr != nil {
		return errors.Join(writeErr, modeErr, closeErr, os.Remove(path))
	}

	target := filepath.Join(binDir, name+executableSuffix(runtime.GOOS))
	if err := os.Rename(path, target); err != nil {
		return errors.Join(fmt.Errorf("install source-built executable: %w", err), os.Remove(path))
	}

	return nil
}
