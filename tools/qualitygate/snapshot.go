// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
)

// snapshotTree copies the repository's tracked and unignored untracked files, as they are on disk
// now, into a fresh directory, so that mutation and control runs work on stable inputs and never
// touch the tree being edited. The caller removes the directory.
func snapshotTree(ctx context.Context, root string) (string, error) {
	parent, err := externalCampaignTemporaryParent(root)
	if err != nil {
		return "", err
	}
	return snapshotTreeAt(ctx, root, parent)
}

func snapshotTreeAt(ctx context.Context, root, parent string) (string, error) {
	listing, err := listSnapshotFiles(ctx, root, snapshotListingBytes)
	if err != nil {
		return "", fmt.Errorf("list repository files: %w", err)
	}

	target, err := os.MkdirTemp(parent, "qualitygate-snapshot-")
	if err != nil {
		return "", fmt.Errorf("create snapshot directory: %w", err)
	}

	copied, err := copyListed(ctx, root, target, listing, newSnapshotCopyBudget())
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
