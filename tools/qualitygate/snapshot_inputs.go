// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	files, err := repopolicy.OwnedGoSources(source)
	if err != nil {
		return err
	}

	copied, err := repopolicy.OwnedGoSources(snapshot)
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

		if inputErr := sameSnapshotInput(source, snapshot, file); inputErr != nil {
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

	return validateSnapshotEmbeds(source, snapshot, output)
}

func sameSnapshotInput(source, snapshot, file string) error {
	original, err := fileReader(source)(file)
	if err != nil {
		return fmt.Errorf("inspect original snapshot input %s: %w", file, err)
	}

	copied, err := fileReader(snapshot)(file)
	if err != nil {
		return fmt.Errorf("%w: snapshot omits input %s; track ignored compiler assets: %w", errGate, file, err)
	}

	if !bytes.Equal(original, copied) {
		return fmt.Errorf("%w: snapshot input changed while copying: %s", errGate, file)
	}

	return nil
}

// validateSnapshotEmbeds uses Go's exact native build/test embedding selections, including fixtures.
func validateSnapshotEmbeds(source, snapshot, output string) error {
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

		if assetErr := validatePackageEmbeds(source, snapshot, resolved, &pkg, checked); assetErr != nil {
			return assetErr
		}
	}
}

func validatePackageEmbeds(source, snapshot, resolved string, pkg *discoveredPackage, checked map[string]bool) error {
	for _, file := range slices.Concat(pkg.EmbedFiles, pkg.TestEmbedFiles, pkg.XTestEmbedFiles) {
		name, relErr := filepath.Rel(resolved, filepath.Join(pkg.Dir, file))
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
		if inputErr := sameSnapshotInput(source, snapshot, name); inputErr != nil {
			return inputErr
		}
	}

	return nil
}
