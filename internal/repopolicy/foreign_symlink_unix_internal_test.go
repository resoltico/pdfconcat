//go:build !windows

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package repopolicy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForeignSourceRejectsFileDirectoryAndArtifactSymlinks(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"file", "root", foreignFixtureZipRole, foreignFixturePatchRole, "record"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			root, source := tinyForeignSource(t)
			name := source.Root + "/reader/value.go"

			switch role {
			case "root":
				name = source.Root
			case foreignFixtureZipRole:
				name = source.SourceArtifact
			case foreignFixturePatchRole:
				name = source.Patch
			case "record":
				name = ForeignSourceRecord
			default: // The regular-file source itself is replaced.
			}

			target := filepath.Join(root, name)

			retained := target + ".retained"
			if err := os.Rename(target, retained); err != nil {
				t.Fatal(err)
			}

			if err := os.Symlink(retained, target); err != nil {
				t.Fatal(err)
			}

			if _, err := ForeignSourcesContext(t.Context(), root); err == nil {
				t.Fatalf("%s symlink was admitted by exact source identity", role)
			}
		})
	}
}
