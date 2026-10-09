// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"context"
	"fmt"
	"os"
	"path"
)

// ReconstructForeignModule stages a verified canonical module ZIP and exact
// patch into an empty owned directory. Supplemental test assets are separate.
func ReconstructForeignModule(ctx context.Context, root string, source ForeignSource, destination string) error {
	if err := validateForeignRecord(source); err != nil {
		return err
	}

	info, statErr := os.Lstat(destination)
	if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: foreign staging requires an owned real directory", errForeignSource)
	}

	entries, directoryErr := os.ReadDir(destination)
	if directoryErr != nil || len(entries) != 0 {
		return fmt.Errorf("%w: foreign staging requires an empty owned directory", errForeignSource)
	}

	return withRoot(root, func(tree *os.Root) error {
		if err := verifyForeignSource(ctx, tree, source); err != nil {
			return err
		}

		archive, err := foreignRegularBytes(tree, source.SourceArtifact)
		if err != nil {
			return err
		}

		patch, err := foreignRegularBytes(tree, source.Patch)
		if err != nil {
			return err
		}

		base, err := foreignArchiveFiles(source, archive)
		if err != nil {
			return err
		}

		_, err = reconstructForeignFiles(ctx, destination, base, patch, path.Base(source.License))

		return err
	})
}
