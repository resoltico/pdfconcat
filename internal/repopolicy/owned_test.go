// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package repopolicy_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestOwnedGoDirectoriesIncludesCompiledFixturesAndHiddenSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{
		"pkg/source.go", "pkg/testdata/decoder/main.go", ".hidden/source.go",
		".tools/cache/source.go", "dist/source.go", "pkg/dist/source.go", "pkg/.tools/source.go",
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(file, []byte("package example\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	dirs, err := repopolicy.OwnedGoDirectories(root)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(dirs, []string{".hidden", "pkg", "pkg/.tools", "pkg/dist", "pkg/testdata/decoder"}) {
		t.Fatalf("owned source discovery: %v", dirs)
	}

	if _, err = repopolicy.OwnedGoDirectories(filepath.Join(root, "missing")); err == nil {
		t.Fatal("unavailable discovery accepted")
	}
}
