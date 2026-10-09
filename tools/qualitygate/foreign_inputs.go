// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

// foreignInputNames includes complete imported trees and their reconstruction
// inputs. Go source selection cannot discover licenses, patches or image fixtures.
func foreignInputNames(ctx context.Context, root string) ([]string, error) {
	sources, err := repopolicy.ForeignSourcesContext(ctx, root)
	if err != nil || len(sources) == 0 {
		return nil, err
	}

	tree, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open foreign inputs: %w", err)
	}
	defer closeLogged(tree)

	names := []string{repopolicy.ForeignSourceRecord}

	for index := range sources {
		source := &sources[index]
		names = append(names, source.SourceArtifact, source.Patch)

		var imported []string

		imported, err = foreignTreeInputNames(ctx, tree, source.Root)
		names = append(names, imported...)

		if err != nil {
			return nil, fmt.Errorf("walk foreign inputs: %w", err)
		}
	}

	slices.Sort(names)

	return slices.Compact(names), nil
}

func foreignTreeInputNames(ctx context.Context, tree *os.Root, root string) ([]string, error) {
	var names []string

	err := fs.WalkDir(tree.FS(), root, func(name string, entry fs.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("foreign input walk canceled: %w", ctxErr)
		}

		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() {
			if !entry.Type().IsRegular() {
				return fmt.Errorf("%w: nonregular foreign input %s", errGate, name)
			}

			names = append(names, name)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk foreign source inputs: %w", err)
	}

	return names, nil
}

func validateForeignSnapshotInputs(ctx context.Context, source, snapshot string) error {
	names, err := foreignInputNames(ctx, source)
	if err != nil {
		return err
	}

	copied, err := foreignInputNames(ctx, snapshot)
	if err != nil {
		return err
	}

	if !slices.Equal(names, copied) {
		return fmt.Errorf("%w: snapshot foreign inputs differ", errGate)
	}

	for _, name := range names {
		if inputErr := sameSnapshotInput(source, snapshot, name); inputErr != nil {
			return inputErr
		}
	}

	return nil
}

func copyForeignInputs(ctx context.Context, source, destination string) error {
	names, err := foreignInputNames(ctx, source)
	if err != nil {
		return err
	}

	if len(names) == 0 {
		return nil
	}

	_, err = copyListed(ctx, source, destination, strings.Join(names, "\x00"))

	return err
}
