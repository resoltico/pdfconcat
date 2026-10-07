// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

const (
	// registryHeader starts every registry fixture.
	registryHeader = "version: 1\ncoverage_threshold_percent: 100\nexceptions:\n"
	// rationaleComment is a valid attached rationale.
	rationaleComment = "  # This exception exists because the repair would contradict the documented project design here.\n"
	// goModFile is the module file at the repository root.
	goModFile = "go.mod"
	// examplePath is the file the registry fixtures point at.
	examplePath = "internal/example/file.go"

	testLinuxOS              = "linux"
	unexpectedProblemsFormat = "unexpected problems: %q"
	directiveSourcePath      = "p/file.go"
	discoveredStatus         = "RUNNABLE"
	fileReadDiagnostic       = "G304: Potential file inclusion"
	broadPatternProblem      = "too broad"
	coverageThresholdField   = "coverage_threshold_percent"
)

// repoRoot returns the repository root: the nearest ancestor of the test directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, goModFile))
		if statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}

		dir = parent
	}
}

// repoReader returns a SourceReader over the repository's files, read through an [os.Root].
func repoReader(t *testing.T) repopolicy.SourceReader {
	t.Helper()

	root := repoRoot(t)

	return func(file string) ([]byte, error) {
		tree, err := os.OpenRoot(root)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", root, err)
		}

		content, readErr := tree.ReadFile(filepath.FromSlash(file))
		if readErr != nil {
			readErr = fmt.Errorf("read %s: %w", file, readErr)
		}

		return content, errors.Join(readErr, tree.Close())
	}
}

// readRepoFile returns the content of a repository file given its slash-separated path.
func readRepoFile(t *testing.T, file string) []byte {
	t.Helper()

	content, err := repoReader(t)(file)
	if err != nil {
		t.Fatal(err)
	}

	return content
}

// sourceFrom returns a SourceReader over an in-memory file map.
func sourceFrom(files map[string]string) repopolicy.SourceReader {
	return func(file string) ([]byte, error) {
		content, found := files[file]
		if !found {
			return nil, os.ErrNotExist
		}

		return []byte(content), nil
	}
}

// requireContains fails the test unless some message contains the wanted text.
func requireContains(t *testing.T, messages []string, want string) {
	t.Helper()

	for _, message := range messages {
		if strings.Contains(message, want) {
			return
		}
	}

	t.Fatalf("no message contains %q; got %q", want, messages)
}

// mustReplace edits text and fails the test when the anchor is absent, so that a negative control
// can never pass because its edit silently did nothing.
func mustReplace(t *testing.T, text, anchor, replacement string) string {
	t.Helper()

	if !strings.Contains(text, anchor) {
		t.Fatalf("the control's anchor %q is not in the document", anchor)
	}

	return strings.Replace(text, anchor, replacement, 1)
}

// sampleRegistry parses entries (each starting with "  - id:") under the standard header, attaching
// the rationale comment the validator requires to every entry.
func sampleRegistry(t *testing.T, entries string) *repopolicy.Registry {
	t.Helper()

	content := registryHeader + strings.ReplaceAll(entries, "\n  - id:", "\n"+rationaleComment+"  - id:")

	registry, err := repopolicy.ParseRegistry([]byte(content))
	if err != nil {
		t.Fatal(err)
	}

	return registry
}
