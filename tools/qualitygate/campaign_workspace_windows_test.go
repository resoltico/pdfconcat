// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCampaignSnapshotCompilesActualEmbedAndRejectsIgnoredInput(t *testing.T) {
	t.Parallel()
	for _, ignored := range []bool{false, true} {
		name := "kept"
		if ignored {
			name = "ignored"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{
				moduleFileName:     snapshotModule,
				snapshotSourceFile: snapshotPackage + "import _ \"embed\"\n//go:embed data.txt\nvar text string\n",
				snapshotDataFile:   "actual embedded fixture bytes",
			}
			if ignored {
				files[snapshotIgnoreFile] = snapshotDataFile + "\n"
			}
			root := snapshotFixture(t, files)
			snapshot, err := snapshotTree(t.Context(), root)
			if snapshot != "" {
				t.Cleanup(func() { removeAll(snapshot) })
			}
			if ignored {
				if err == nil || !strings.Contains(err.Error(), snapshotDataFile) {
					t.Fatalf("ignored actual embed was accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			list, err := goCommand(snapshot, "list", "-deps", "-f", `{{.ImportPath}} {{join .EmbedFiles ","}}`, "./...").output(t.Context())
			if err != nil || !strings.Contains(list, "snapshot.fixture "+snapshotDataFile+"\n") {
				t.Fatalf("actual Go snapshot embed discovery: %v\n%s", err, list)
			}
			if output, err := goCommand(snapshot, "build", "./...").output(t.Context()); err != nil {
				t.Fatalf("actual Go snapshot embed build: %v\n%s", err, output)
			}
		})
	}
}

func TestWindowsCampaignDirectoryRejectsDifferentOrdinaryIdentity(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	qualified := `\\?\` + base
	if strings.HasPrefix(base, `\\`) {
		qualified = `\\?\UNC\` + base[2:]
	}
	ordinary := filepath.Join(base, "literal")
	if err := os.Mkdir(ordinary, dirMode); err != nil {
		t.Fatal(err)
	}
	literal := qualified + `\literal.`
	if err := os.Mkdir(literal, dirMode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(literal) })
	selected, err := os.Stat(literal)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.Stat(ordinary)
	if err != nil || os.SameFile(selected, other) {
		t.Fatalf("literal extended fixture did not select a distinct native directory: %v", err)
	}
	if path, err := nativeCampaignDirectory(literal); path != "" || !errors.Is(err, errGate) {
		t.Fatalf("ordinary spelling redirected native directory: %q/%v", path, err)
	}
}
