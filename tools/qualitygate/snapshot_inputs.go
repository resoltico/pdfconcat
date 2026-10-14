// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// validateSnapshotInputs rejects omitted owned source before compiler discovery can hide it.
func validateSnapshotInputs(ctx context.Context, source, snapshot string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("snapshot input validation canceled: %w", ctxErr)
	}

	if err := validateForeignSnapshotInputs(ctx, source, snapshot); err != nil {
		return err
	}

	files, err := repopolicy.OwnedGoSources(ctx, source)
	if err != nil {
		return err
	}

	copied, err := repopolicy.OwnedGoSources(ctx, snapshot)
	if err != nil {
		return err
	}

	if !slices.Equal(files, copied) {
		return fmt.Errorf("%w: snapshot owned Go inputs differ; track ignored source and keep inputs stable", errGate)
	}

	for _, file := range files {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("snapshot input comparison canceled: %w", ctxErr)
		}

		if inputErr := sameSnapshotInput(ctx, source, snapshot, file); inputErr != nil {
			return inputErr
		}
	}

	packages, err := ownedPackages(ctx, source)
	if err != nil {
		return err
	}

	output, err := goCommand(source, append([]string{goListVerb, jsonFlag, "-test"}, packages...)...).output(ctx)
	if err != nil {
		return fmt.Errorf("discover snapshot compiler assets: %w", err)
	}

	return validateSnapshotEmbeds(ctx, source, snapshot, output)
}

func sameSnapshotInput(ctx context.Context, source, snapshot, file string) (result error) {
	from, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, from.Close()) }()
	dest, err := os.OpenRoot(snapshot)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, dest.Close()) }()
	original, originalInfo, err := openSnapshotInput(from, file)
	if err != nil {
		return fmt.Errorf("inspect original snapshot input %s: %w", file, err)
	}
	defer func() { result = errors.Join(result, original.Close()) }()
	copied, copiedInfo, err := openSnapshotInput(dest, file)
	if err != nil {
		return fmt.Errorf("%w: snapshot omits input %s; track ignored compiler assets: %w", errGate, file, err)
	}
	defer func() { result = errors.Join(result, copied.Close()) }()
	if copiedInfo.Size() > snapshotCopyBytes || originalInfo.Size() != copiedInfo.Size() {
		return fmt.Errorf("%w: snapshot input changed size while copying: %s", errGate, file)
	}
	if err = compareSnapshotStreams(ctx, original, copied, copiedInfo.Size()); err != nil {
		return fmt.Errorf("snapshot input changed while copying: %s: %w", file, err)
	}
	return nil
}

// validateSnapshotEmbeds uses Go's exact native build/test embedding selections, including fixtures.
func validateSnapshotEmbeds(ctx context.Context, source, snapshot, output string) error {
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return fmt.Errorf("resolve snapshot source: %w", err)
	}

	decoder := json.NewDecoder(strings.NewReader(output))
	checked := map[string]bool{}
	packages := 0

	for {
		var pkg discoveredPackage

		if decodeErr := decoder.Decode(&pkg); errors.Is(decodeErr, io.EOF) {
			if packages == 0 {
				return fmt.Errorf("%w: empty snapshot compiler asset discovery", errGate)
			}

			return nil
		} else if decodeErr != nil {
			return fmt.Errorf("decode snapshot compiler assets: %w", decodeErr)
		}

		packages++

		if pkg.Dir == "" {
			return fmt.Errorf("%w: compiler asset package lacks directory", errGate)
		}

		if assetErr := validatePackageEmbeds(ctx, source, snapshot, resolved, &pkg, checked); assetErr != nil {
			return assetErr
		}
	}
}

func validatePackageEmbeds(ctx context.Context, source, snapshot, resolved string, pkg *discoveredPackage, checked map[string]bool) error {
	directory, err := filepath.EvalSymlinks(pkg.Dir)
	if err != nil {
		return fmt.Errorf("resolve compiler asset package directory: %w", err)
	}

	for _, file := range slices.Concat(pkg.EmbedFiles, pkg.TestEmbedFiles, pkg.XTestEmbedFiles) {
		name, relErr := filepath.Rel(resolved, filepath.Join(directory, file))
		if relErr != nil {
			return fmt.Errorf("compiler asset path: %w", relErr)
		}

		if name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: compiler asset outside snapshot source: %s/%s", errGate, pkg.Dir, file)
		}

		name = filepath.ToSlash(name)
		if checked[name] {
			continue
		}

		checked[name] = true
		if inputErr := sameSnapshotInput(ctx, source, snapshot, name); inputErr != nil {
			return inputErr
		}
	}

	return nil
}
