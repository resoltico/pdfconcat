// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const secretsCommand = "secrets"

// runSecrets checks intended source and every reachable historical commit without printing credentials.
func runSecrets(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("%w: secrets takes no arguments", errGate)
	}

	root, rootErr := repoRoot()
	if rootErr != nil {
		return rootErr
	}

	tool, toolErr := findTool(root, "gitleaks")
	if toolErr != nil {
		return toolErr
	}

	versions, pinErr := readToolVersions(root)
	if pinErr != nil {
		return pinErr
	}

	printed, versionErr := (&command{name: tool, args: []string{versionVerb}}).output(ctx)
	if versionErr != nil || strings.TrimSpace(printed) != strings.TrimPrefix(versions["GITLEAKS_VERSION"], "v") {
		return fmt.Errorf("%w: gitleaks must match tools/versions.env; run go run ./tools/installtools gitleaks", errGate)
	}

	source, snapshotErr := snapshotTree(ctx, root)
	if snapshotErr != nil {
		return snapshotErr
	}
	defer removeAll(source)

	return scanSecretScopes(ctx, tool, root, source)
}

func scanSecretScopes(ctx context.Context, tool, history, source string) error {
	scratch, scratchErr := os.MkdirTemp("", "qualitygate-secrets-")
	if scratchErr != nil {
		return fmt.Errorf("create scanner configuration directory: %w", scratchErr)
	}
	defer removeAll(scratch)

	config := filepath.Join(scratch, "default-secret-rules.toml")
	if writeErr := os.WriteFile(config, []byte("[extend]\nuseDefault = true\n"), fileMode); writeErr != nil {
		return fmt.Errorf("write scanner default configuration: %w", writeErr)
	}

	common := []string{
		"--config", config, "--redact=100", "--no-banner", "--no-color",
		"--ignore-gitleaks-allow", "--gitleaks-ignore-path", filepath.Join(scratch, "no-secret-ignores"),
	}
	for _, scope := range []struct{ mode, path string }{{"dir", source}, {"git", history}} {
		args := append([]string{scope.mode, scope.path}, common...)
		if scope.mode == "git" {
			args = append(args, "--log-opts=--all --full-history")
		}

		scan := command{
			name: tool, args: args, stdout: log.Writer(), stderr: log.Writer(),
			env: []string{"GITLEAKS_CONFIG=", "GITLEAKS_CONFIG_TOML="},
		}
		if scanErr := scan.run(ctx); scanErr != nil {
			return fmt.Errorf("%s secret scan failed: %w", scope.mode, scanErr)
		}
	}

	log.Print(
		"secrets: intended source and reachable Git history passed; scanner detections are not proof that every secret format is covered",
	)

	return nil
}
