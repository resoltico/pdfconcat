// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package publish_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/publish"
)

const filePermissions = 0o600

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), filePermissions)
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		closeErr := root.Close()
		if closeErr != nil {
			t.Error(closeErr)
		}
	}()

	content, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

func TestFileRefusesExistingDestinationWithoutOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staged, destination := filepath.Join(dir, stagedPDFPath), filepath.Join(dir, outputPath)
	writeFile(t, staged, replacementContent)
	writeFile(t, destination, originalContent)

	err := publish.File(staged, destination, false)
	if err == nil {
		t.Fatal("File() replaced an existing destination without overwrite")
	}

	if got := readFile(t, destination); got != originalContent {
		t.Fatalf("destination = %q, want old content", got)
	}
}

func TestFileCreatesNewDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staged, destination := filepath.Join(dir, stagedPDFPath), filepath.Join(dir, outputPath)
	writeFile(t, staged, "verified")

	err := publish.File(staged, destination, false)
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}

	if got := readFile(t, destination); got != "verified" {
		t.Fatalf("destination = %q, want verified content", got)
	}
}

func TestFileReplacesExistingDestinationWithOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staged, destination := filepath.Join(dir, stagedPDFPath), filepath.Join(dir, outputPath)
	writeFile(t, staged, replacementContent)
	writeFile(t, destination, originalContent)

	err := publish.File(staged, destination, true)
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}

	if got := readFile(t, destination); got != replacementContent {
		t.Fatalf("destination = %q, want new content", got)
	}
}

func TestFileRequiresBothPaths(t *testing.T) {
	t.Parallel()

	if publish.File("", outputPath, false) == nil || publish.File(stagedPDFPath, "", false) == nil {
		t.Fatal("File() accepted an empty path")
	}
}

func TestFileNamesTheMissingPathWhicheverIsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	staged := filepath.Join(dir, stagedPDFPath)
	writeFile(t, staged, stagedContent)

	for name, paths := range map[string][2]string{
		"staged path empty":      {"", filepath.Join(dir, outputPath)},
		"destination path empty": {staged, ""},
		"both paths empty":       {"", ""},
	} {
		err := publish.File(paths[0], paths[1], false)
		if !errors.Is(err, publish.ErrPathRequired) {
			t.Errorf("%s: File() = %v", name, err)
		}
	}

	if readFile(t, staged) != stagedContent {
		t.Error("a refused publication touched the staged file")
	}
}

func TestCheckDestinationRejectsSymbolicLink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target, link := filepath.Join(dir, "target.pdf"), filepath.Join(dir, outputPath)
	writeFile(t, target, "target")

	err := os.Symlink(target, link)
	if err != nil {
		t.Fatalf("required symbolic-link capability unavailable: %v", err)
	}

	err = publish.CheckDestination(link, true)
	if err == nil {
		t.Fatal("CheckDestination() accepted symbolic-link output with overwrite")
	}
}

func TestCheckDestinationRejectsUnusableLocations(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	notADirectory := filepath.Join(dir, "file.pdf")
	writeFile(t, notADirectory, "x")

	tests := map[string]string{
		"empty path":              "",
		"missing directory":       filepath.Join(dir, missingPath, outputPath),
		"directory is a file":     filepath.Join(notADirectory, outputPath),
		"destination is a folder": dir,
	}
	for name, destination := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := publish.CheckDestination(destination, true)
			if err == nil {
				t.Fatalf("CheckDestination(%q) succeeded", destination)
			}
		})
	}
}

func TestCheckDestinationAllowsNewAndExplicitlyReplaceableFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.pdf")
	writeFile(t, existing, "x")

	err := publish.CheckDestination(filepath.Join(dir, "new.pdf"), false)
	if err != nil {
		t.Errorf("new destination rejected: %v", err)
	}

	err = publish.CheckDestination(existing, true)
	if err != nil {
		t.Errorf("replaceable destination rejected: %v", err)
	}

	err = publish.CheckDestination(existing, false)
	if err == nil {
		t.Error("existing destination accepted without overwrite")
	}
}
