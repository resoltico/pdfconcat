// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepositoryIssues checks, without running any tool, that every entry's path, function and anchor
// still exist in the source tree. An entry that points at code which has been renamed or removed
// fails here, before the slower gates would report it as stale.
func RepositoryIssues(entries []*Entry, read SourceReader) []string {
	var problems []string

	for _, entry := range entries {
		if entry.Path == "" {
			continue
		}

		content, err := read(entry.Path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: cannot read %s: %v", entry.ID, entry.Path, err))

			continue
		}

		if issue := repositorySourceIssue(entry, content); issue != "" {
			problems = append(problems, entry.ID+": "+issue)
		}
	}

	return problems
}

func repositorySourceIssue(entry *Entry, content []byte) string {
	if entry.Tool == ToolMutation {
		if _, err := mutationAnchorLine(entry, content); err != nil {
			return err.Error()
		}

		return ""
	}

	first, last := 1, strings.Count(string(content), "\n")+1

	if entry.Tool == ToolCoverage {
		var err error

		first, last, err = functionLines(entry.Path, content, entry.Function)
		if err != nil {
			return err.Error()
		}
	}

	if entry.Anchor != "" && !anchorWithin(content, entry.Anchor, first, last) {
		return fmt.Sprintf("no line of %s in %s equals the anchor %q", describeScope(entry), entry.Path, entry.Anchor)
	}

	return ""
}

// describeScope names where an anchor was searched: the function for coverage entries, the file otherwise.
func describeScope(entry *Entry) string {
	if entry.Function != "" {
		return entry.Function
	}

	return "the file"
}

// anchorWithin reports whether a line between first and last (1-based, inclusive) equals the anchor once trimmed.
func anchorWithin(content []byte, anchor string, first, last int) bool {
	for number, line := range strings.Split(string(content), "\n") {
		if number+1 >= first && number+1 <= last && strings.TrimSpace(line) == anchor {
			return true
		}
	}

	return false
}

// withRoot opens dir as an [os.Root], runs use with it, and closes it, reporting the errors of both.
// Reading through a root keeps a symbolic link from leading a read outside dir.
func withRoot(dir string, use func(tree *os.Root) error) error {
	tree, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}

	return errors.Join(use(tree), tree.Close())
}

// readFile reads one file through a root opened on its directory.
func readFile(file string) ([]byte, error) {
	var content []byte

	err := withRoot(filepath.Dir(file), func(tree *os.Root) error {
		var readErr error

		content, readErr = tree.ReadFile(filepath.Base(file))
		if readErr != nil {
			return fmt.Errorf("read file: %w", readErr)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}

	return content, nil
}
