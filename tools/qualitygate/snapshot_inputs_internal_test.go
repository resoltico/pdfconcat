// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	snapshotIgnoreFile  = ".gitignore"
	snapshotModule      = "module snapshot.fixture\ngo 1.27.1\n"
	snapshotPackage     = "package snapshotfixture\n"
	snapshotSourceFile  = "source.go"
	snapshotDeletedFile = "deleted.go"
	snapshotDataFile    = "data.txt"
	snapshotConfigFile  = "snapshot-config.json"
)

func snapshotFixture(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(text), fileMode); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := (&command{dir: root, name: gitTool, args: []string{"init", "-q"}}).output(t.Context()); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestSnapshotRejectsIgnoredGoBeforeCompilerDiscovery(t *testing.T) {
	t.Parallel()

	root := snapshotFixture(t, map[string]string{
		moduleFileName: snapshotModule, snapshotIgnoreFile: "ignored.go\n",
		snapshotSourceFile: snapshotPackage, "ignored.go": "not valid Go syntax\n",
	})
	if snapshot, err := snapshotTree(t.Context(), root); err == nil || !strings.Contains(err.Error(), "owned Go inputs differ") {
		removeAll(snapshot)
		t.Fatalf("ignored owned input hidden by compiler discovery: %v", err)
	}
}

func TestSnapshotRejectsIgnoredBuildAndTestEmbeds(t *testing.T) {
	t.Parallel()

	for _, source := range []string{snapshotSourceFile, "source_test.go", "snapshot_external_test.go"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			pkg := "snapshotfixture"
			if source == "snapshot_external_test.go" {
				pkg += "_test"
			}

			files := map[string]string{
				moduleFileName: snapshotModule, snapshotIgnoreFile: "assets/ignored.json\n",
				snapshotSourceFile: snapshotPackage, "assets/kept.json": "{}", "assets/ignored.json": "hidden",
			}
			files[source] = "package " + pkg + "\nimport \"embed\"\n//go:embed assets/*\nvar assets embed.FS\n"

			root := snapshotFixture(t, files)
			if snapshot, err := snapshotTree(t.Context(), root); err == nil || !strings.Contains(err.Error(), "assets/ignored.json") {
				removeAll(snapshot)
				t.Fatalf("ignored embedded asset hidden: %v", err)
			}
		})
	}
}

func TestSnapshotPreservesWorkingTreeDeletionAndRegularAssets(t *testing.T) {
	t.Parallel()

	root := snapshotFixture(t, map[string]string{
		moduleFileName: snapshotModule, snapshotDeletedFile: snapshotPackage,
		snapshotSourceFile: "package snapshotfixture\nimport _ \"embed\"\n//go:embed data.txt\nvar data string\n",
		snapshotDataFile:   "current data", snapshotConfigFile: "{\"mode\":\"test\"}", snapshotIgnoreFile: "/bin/\n",
		"bin/artifact": "ignored build output",
	})
	if _, err := (&command{dir: root, name: gitTool, args: []string{"add", snapshotDeletedFile}}).output(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, snapshotDeletedFile)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := snapshotTree(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer removeAll(snapshot)

	for _, name := range []string{snapshotSourceFile, snapshotDataFile, snapshotConfigFile} {
		if inputErr := sameSnapshotInput(root, snapshot, name); inputErr != nil {
			t.Fatal(inputErr)
		}
	}

	for _, name := range []string{snapshotDeletedFile, "bin/artifact"} {
		if _, statErr := os.Stat(filepath.Join(snapshot, name)); !os.IsNotExist(statErr) {
			t.Fatalf("deleted/ignored artifact copied: %s %v", name, statErr)
		}
	}
}

func TestSnapshotRejectsChangedBytesAndEscapingEmbed(t *testing.T) {
	t.Parallel()

	root, snapshot := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, snapshotDataFile), []byte("old"), fileMode); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(snapshot, snapshotDataFile), []byte("different"), fileMode); err != nil {
		t.Fatal(err)
	}

	if err := sameSnapshotInput(root, snapshot, snapshotDataFile); err == nil {
		t.Fatal("changed bytes accepted")
	}

	listing := `{"Dir":"` + filepath.ToSlash(root) + `","EmbedFiles":["../outside"]}`
	if err := validateSnapshotEmbeds(root, snapshot, listing); err == nil {
		t.Fatal("compiler asset escaped rooted snapshot")
	}

	for _, malformed := range []string{"", "{}", "{"} {
		if err := validateSnapshotEmbeds(root, snapshot, malformed); err == nil {
			t.Fatalf("invalid compiler asset discovery accepted: %q", malformed)
		}
	}
}

func TestSnapshotRejectsCompilerDiscoveryFailure(t *testing.T) {
	t.Parallel()

	root := snapshotFixture(t, map[string]string{
		moduleFileName: snapshotModule, snapshotSourceFile: "not valid Go syntax\n",
	})
	if snapshot, err := snapshotTree(t.Context(), root); err == nil {
		removeAll(snapshot)
		t.Fatal("compiler discovery failure accepted")
	}
}
