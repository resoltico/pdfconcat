// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	gremlinsSourceModule = repopolicy.GremlinsModule
	gremlinsSourcePrefix = "GREMLINS"
	gremlinsPatchPath    = "tools/mutation-patches/gremlins-executor.patch"
)

func installGremlins(ctx context.Context, root string, versions map[string]string, binDir string) (failure error) {
	identity, err := repopolicy.GremlinsBuildIdentity(versions)
	if err != nil {
		return err
	}

	workspace, err := os.MkdirTemp("", "pdfconcat-gremlins-build-*")
	if err != nil {
		return fmt.Errorf("create isolated Gremlins source directory: %w", err)
	}

	defer func() { failure = errors.Join(failure, os.RemoveAll(workspace)) }()

	tree, err := prepareGremlinsSource(ctx, root, workspace, versions)
	if err != nil {
		return err
	}

	output := filepath.Join(workspace, "gremlins"+executableSuffix(runtime.GOOS))
	if buildErr := buildGremlinsSource(ctx, tree, output, identity.Version); buildErr != nil {
		return buildErr
	}

	if verifyErr := verifyBuiltGremlins(ctx, output, identity.Version); verifyErr != nil {
		return verifyErr
	}

	content, err := readFile(workspace, filepath.Base(output))
	if err != nil {
		return err
	}

	if installErr := installSourceExecutable(binDir, "gremlins", content); installErr != nil {
		return installErr
	}

	log.Printf(
		"Gremlins %s installed from verified upstream source and reviewed executor patch; dependency files unchanged",
		identity.Version,
	)

	return nil
}

func prepareGremlinsSource(ctx context.Context, root, workspace string, versions map[string]string) (string, error) {
	source, archive, err := downloadModuleSource(ctx, versions, workspace, gremlinsSourceModule, gremlinsSourcePrefix)
	if err != nil {
		return "", err
	}

	if timeErr := verifySourceTime(source, versions["GREMLINS_SOURCE_TIME"]); timeErr != nil {
		return "", timeErr
	}

	tree := filepath.Join(workspace, "source")
	if mkdirErr := os.Mkdir(tree, dirMode); mkdirErr != nil {
		return "", fmt.Errorf("create Gremlins source tree: %w", mkdirErr)
	}

	if extractErr := extractModuleSource(archive, gremlinsSourceModule, versions["GREMLINS_VERSION"], tree); extractErr != nil {
		return "", extractErr
	}

	locked, err := snapshotSourceDependencies(tree)
	if err != nil {
		return "", err
	}

	patch, err := readFile(root, gremlinsPatchPath)
	if err != nil {
		return "", err
	}

	if patchErr := applySourcePatch(ctx, tree, patch, versions["GREMLINS_PATCH_SHA256"]); patchErr != nil {
		return "", patchErr
	}

	return tree, locked.verify(tree)
}

func buildGremlinsSource(ctx context.Context, tree, output, version string) error {
	locked, err := snapshotSourceDependencies(tree)
	if err != nil {
		return err
	}

	tests := gremlinsSourceTestArgs(runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == windowsOS && runtime.GOARCH == arm64Arch {
		log.Print("Gremlins controls use ordinary tests: Windows/arm64 has no Go race detector")
	}

	for _, args := range [][]string{{sourceModVerb, sourceDownloadVerb}, {sourceModVerb, "verify"}, tests} {
		if stepErr := runSourceGo(ctx, tree, args...); stepErr != nil {
			return stepErr
		}

		if verifyErr := locked.verify(tree); verifyErr != nil {
			return verifyErr
		}
	}

	buildErr := runSourceGo(
		ctx,
		tree,
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags",
		"-X main.version="+version,
		"-o",
		output,
		"./cmd/gremlins",
	)
	if buildErr != nil {
		return buildErr
	}

	return locked.verify(tree)
}

func verifyBuiltGremlins(ctx context.Context, file, version string) error {
	output, err := exec.CommandContext(ctx, file, "--version").Output()
	if err != nil {
		return fmt.Errorf("verify Gremlins source variant: %w", err)
	}

	if strings.TrimSpace(string(output)) != "gremlins version "+version+" "+runtime.GOOS+"/"+runtime.GOARCH {
		return fmt.Errorf("%w: Gremlins binary does not report the exact reviewed variant and host target", errInstall)
	}

	return repopolicy.VerifyGremlinsBinaryMetadata(file)
}

// All six packages own required judgment, lifecycle, pool, descriptor and reporting controls.
func gremlinsSourceTestArgs(goos, goarch string) []string {
	args := []string{sourceTestVerb}
	if goos != windowsOS || goarch != arm64Arch {
		args = append(args, "-race")
	}

	return append(args, "./internal/execution", "./internal/coverage", "./internal/engine",
		"./internal/engine/workerpool", "./internal/engine/workdir", "./internal/report")
}
