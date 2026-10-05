// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// moduleRoot returns the repository root: the nearest ancestor holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
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

func readLines(t *testing.T, path string) []string {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = file.Close() }()

	var lines []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	err = scanner.Err()
	if err != nil {
		t.Fatal(err)
	}

	return lines
}

// TestNoInlineLintExceptions enforces the policy that exceptions are never written next to the code.
func TestNoInlineLintExceptions(t *testing.T) {
	t.Parallel()

	// Assembled at run time so that this file does not match its own search.
	directive := regexp.MustCompile(`//\s*` + "no" + `lint\b`)
	root := moduleRoot(t)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "third_party") {
			return filepath.SkipDir
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		for number, line := range readLines(t, path) {
			if directive.MatchString(line) {
				relative, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d: inline lint exception; add it to the registry in .golangci.yml instead", relative, number+1)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

var (
	// exceptionEntry matches list items under the registry keys and per-rule disable switches.
	exceptionEntry = regexp.MustCompile(`^\s+(- [A-Za-z0-9_-]+|- path:.*|- \{name: [a-z-]+, disabled: true\})`)
	registryKey    = regexp.MustCompile(`^\s+(disable|disabled-checks|rules):\s*$`)
)

// TestEveryRegistryExceptionIsJustified requires a comment for each exception in .golangci.yml:
// either trailing on the entry's own line, or directly above it.
func TestEveryRegistryExceptionIsJustified(t *testing.T) {
	t.Parallel()

	lines := readLines(t, filepath.Join(moduleRoot(t), ".golangci.yml"))
	inRegistry := false
	checked := 0

	for index, line := range lines {
		switch {
		case registryKey.MatchString(line):
			inRegistry = true

			continue
		case strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#"):
			continue
		case !strings.HasPrefix(strings.TrimSpace(line), "- "):
			inRegistry = false
		default:
		}

		if !inRegistry || !exceptionEntry.MatchString(line) {
			continue
		}

		checked++

		if !justified(lines, index) {
			t.Errorf(".golangci.yml:%d: exception has no comment explaining it: %s", index+1, strings.TrimSpace(line))
		}
	}

	if checked == 0 {
		t.Fatal("found no registry exceptions to check; the parser has drifted from the file layout")
	}
}

// justified reports whether the line at index carries a trailing comment or sits directly under
// a comment block. Each entry needs its own justification; a comment never covers siblings below it.
func justified(lines []string, index int) bool {
	if strings.Contains(lines[index], " #") {
		return true
	}

	return index > 0 && strings.HasPrefix(strings.TrimSpace(lines[index-1]), "#")
}
