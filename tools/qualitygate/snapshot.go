// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// snapshotTree copies the repository's tracked and unignored untracked files, as they are on disk
// now, into a fresh directory, so that mutation and control runs work on stable inputs and never
// touch the tree being edited. The caller removes the directory.
func snapshotTree(ctx context.Context, root string) (string, error) {
	lsFiles := []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard"}

	listing, err := (&command{dir: root, name: gitTool, args: lsFiles}).output(ctx)
	if err != nil {
		return "", fmt.Errorf("list repository files: %w", err)
	}

	target, err := os.MkdirTemp("", "qualitygate-snapshot-")
	if err != nil {
		return "", fmt.Errorf("create snapshot directory: %w", err)
	}

	copied, err := copyListed(ctx, root, target, listing)
	if err != nil {
		removeAll(target)

		return "", err
	}

	if copied == 0 {
		removeAll(target)

		return "", fmt.Errorf("%w: the snapshot is empty: git listed no files", errGate)
	}

	if inputErr := validateSnapshotInputs(ctx, root, target); inputErr != nil {
		removeAll(target)

		return "", inputErr
	}

	return target, nil
}

// copyListed copies each NUL-separated file name of listing from source to target and counts the copies.
// Both directories are opened as [os.Root], so no listed name can reach outside them.
func copyListed(ctx context.Context, source, target, listing string) (int, error) {
	from, err := os.OpenRoot(source)
	if err != nil {
		return 0, fmt.Errorf(openFileError, source, err)
	}

	dest, err := os.OpenRoot(target)
	if err != nil {
		return 0, errors.Join(fmt.Errorf(openFileError, target, err), from.Close())
	}

	copied := 0

	var copyErr error

	for name := range strings.SplitSeq(listing, "\x00") {
		if ctxErr := ctx.Err(); ctxErr != nil {
			copyErr = ctxErr
			break
		}

		if name == "" {
			continue
		}

		err = copyFile(ctx, from, dest, filepath.FromSlash(name))
		if errors.Is(err, fs.ErrNotExist) {
			continue // Listed by git but deleted from disk: absent from the snapshot, as in the tree.
		}

		if err != nil {
			copyErr = err

			break
		}

		copied++
	}

	return copied, errors.Join(copyErr, from.Close(), dest.Close())
}

func copyFile(ctx context.Context, from, dest *os.Root, name string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("snapshot copy canceled: %w", ctxErr)
	}

	info, err := from.Lstat(name)
	if err != nil {
		return fmt.Errorf("stat %s: %w", name, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: snapshot input is not a regular file: %s", errGate, name)
	}

	content, err := from.ReadFile(name)
	if err != nil {
		return fmt.Errorf(readFileError, name, err)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("snapshot copy canceled: %w", ctxErr)
	}

	err = dest.MkdirAll(filepath.Dir(name), dirMode)
	if err != nil {
		return fmt.Errorf("create directory for %s: %w", name, err)
	}

	err = dest.WriteFile(name, content, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}

	return nil
}

// replaceAnchoredLine replaces the one line of content whose trimmed text equals anchor with
// replacement, keeping the line's indentation. It fails, naming the file, unless the anchor occurs
// exactly once, so that a refactor that moves or duplicates the line is noticed rather than ignored.
func replaceAnchoredLine(file string, content []byte, anchor, replacement string) ([]byte, error) {
	lines := bytes.Split(content, []byte("\n"))
	found := -1
	count := 0

	for index, line := range lines {
		if strings.TrimSpace(string(line)) == anchor {
			found = index
			count++
		}
	}

	if count != 1 {
		return nil, fmt.Errorf("%w: the anchor %q occurs %d times in %s, want exactly 1; update the control to the current code",
			errGate, anchor, count, file)
	}

	original := string(lines[found])
	indentation := original[:len(original)-len(strings.TrimLeft(original, " \t"))]
	lines[found] = []byte(indentation + replacement)

	return bytes.Join(lines, []byte("\n")), nil
}
