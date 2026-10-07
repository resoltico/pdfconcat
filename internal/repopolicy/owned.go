// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
)

// OwnedGoDirectories finds the source directories that lint/format must consider, including hidden
// and testdata fixture packages omitted by Go's ./... pattern. It shares the directive scan's owned
// filesystem boundary; compiler build-tag filtering remains Go's responsibility.
func OwnedGoDirectories(root string) ([]string, error) {
	files, err := OwnedGoSources(root)
	if err != nil {
		return nil, err
	}

	directories := map[string]bool{}
	for _, file := range files {
		directories[path.Dir(file)] = true
	}

	return slices.Sorted(maps.Keys(directories)), nil
}

// OwnedGoSources returns all owned Go files, including platform variants and standalone fixtures.
func OwnedGoSources(root string) ([]string, error) {
	var files []string

	err := withRoot(root, func(tree *os.Root) error {
		return fs.WalkDir(tree.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() && skippedDirectories()[entry.Name()] {
				return fs.SkipDir
			}

			if !entry.IsDir() && strings.HasSuffix(name, goSourceSuffix) {
				files = append(files, name)
			}

			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("discover owned Go sources: %w", err)
	}

	slices.Sort(files)

	return files, nil
}
