// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const linterPatchPath = "tools/lint-patches/golangci-lint-source-contracts.patch"

func installLinter(ctx context.Context, root string, versions map[string]string, binDir string) (failure error) {
	identity, identityErr := repopolicy.GolangciBuildIdentity(versions)
	if identityErr != nil {
		return fmt.Errorf("derive linter build identity: %w", identityErr)
	}

	workspace, workErr := os.MkdirTemp("", "pdfconcat-linter-build-*")
	if workErr != nil {
		return fmt.Errorf("create isolated linter build directory: %w", workErr)
	}

	defer func() { failure = errors.Join(failure, os.RemoveAll(workspace)) }()

	tree, prepareErr := prepareLinterSource(ctx, root, workspace, versions)
	if prepareErr != nil {
		return prepareErr
	}

	output := filepath.Join(workspace, "golangci-lint"+executableSuffix(runtime.GOOS))
	if err := buildLinterSource(ctx, tree, output, identity); err != nil {
		return err
	}

	if err := verifyBuiltLinter(ctx, output, identity); err != nil {
		return err
	}

	executable, readErr := readFile(workspace, filepath.Base(output))
	if readErr != nil {
		return readErr
	}

	if err := installSourceExecutable(binDir, "golangci-lint", executable); err != nil {
		return err
	}

	log.Printf("golangci-lint %s installed from checksum-verified upstream source; "+
		"reviewed patch SHA256 %s; upstream go.mod/go.sum unchanged", identity.Version, versions["GOLANGCI_LINT_PATCH_SHA256"])

	return nil
}

func prepareLinterSource(ctx context.Context, root, workspace string, versions map[string]string) (string, error) {
	source, archive, sourceErr := downloadModuleSource(ctx, versions, workspace, linterModule, linterPinPrefix)
	if sourceErr != nil {
		return "", sourceErr
	}

	if err := verifySourceTime(source, versions["GOLANGCI_LINT_SOURCE_TIME"]); err != nil {
		return "", err
	}

	tree := filepath.Join(workspace, "source")
	if err := os.Mkdir(tree, dirMode); err != nil {
		return "", fmt.Errorf("create source directory: %w", err)
	}

	if err := extractModuleSource(archive, linterModule, versions["GOLANGCI_LINT_VERSION"], tree); err != nil {
		return "", err
	}

	original, lockErr := snapshotSourceDependencies(tree)
	if lockErr != nil {
		return "", lockErr
	}

	for _, args := range [][]string{{sourceModVerb, sourceDownloadVerb}, {sourceModVerb, "verify"}, {sourceModVerb, "vendor"}} {
		if err := runSourceGo(ctx, tree, args...); err != nil {
			return "", err
		}

		if err := original.verify(tree); err != nil {
			return "", err
		}
	}

	patch, patchErr := readFile(root, linterPatchPath)
	if patchErr != nil {
		return "", patchErr
	}

	if err := applySourcePatch(ctx, tree, patch, versions["GOLANGCI_LINT_PATCH_SHA256"]); err != nil {
		return "", err
	}

	if err := original.verify(tree); err != nil {
		return "", err
	}

	return tree, nil
}

func verifyBuiltLinter(ctx context.Context, file string, identity repopolicy.SourceBuildIdentity) error {
	command := exec.CommandContext(ctx, file, "version", "--json")

	var output bytes.Buffer

	command.Stdout = &output

	command.Stderr = log.Writer()
	if runErr := command.Run(); runErr != nil {
		return fmt.Errorf("verify built linter identity: %w", runErr)
	}

	if err := repopolicy.VerifyGolangciBinaryMetadata(file); err != nil {
		return err
	}

	return repopolicy.VerifyGolangciReportedIdentity(output.Bytes(), identity)
}
