// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build !windows

package repopolicy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func TestOwnedSourceDiscoveryRejectsDirectoryAndBrokenLinks(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"dist/hidden", "broken-source-target"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "dist", "hidden"), 0o750); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
				t.Fatal(err)
			}

			if _, err := repopolicy.OwnedGoSources(root); err == nil {
				t.Fatal("uninspectable source link accepted")
			}

			if _, err := repopolicy.ScanDirectives(root); err == nil {
				t.Fatal("directive scan missed source link")
			}
		})
	}
}

func TestOwnedSourceDiscoveryAllowsRootedRegularFileLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package linkedsource\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("note"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, pair := range [][2]string{{"source.go", "linked.go"}, {"note.txt", "linked.txt"}} {
		if err := os.Symlink(pair[0], filepath.Join(root, pair[1])); err != nil {
			t.Fatal(err)
		}
	}

	sources, err := repopolicy.OwnedGoSources(root)
	if err != nil || len(sources) != 2 {
		t.Fatalf("regular link rejected: %v, %v", sources, err)
	}

	if issues, scanErr := repopolicy.ScanSourceLimits(root); scanErr != nil || len(issues) != 0 {
		t.Fatalf("source link not scanned: %v, %v", issues, scanErr)
	}
}
