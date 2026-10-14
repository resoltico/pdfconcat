// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCampaignScratchClosedLeasePreservesCleanupFailureAndData(t *testing.T) {
	t.Parallel()
	scratch, err := newCampaignScratch(t.TempDir(), "qualitygate-closed-root-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(scratch.path) })
	if err = scratch.root.WriteFile("retained", []byte("owned data"), fileMode); err != nil {
		t.Fatal(err)
	}
	// Exercise a real closed-root error rather than a cleanup callback or mock.
	if err = scratch.root.Close(); err != nil {
		t.Fatal(err)
	}
	if err = scratch.close(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed lease cleanup failure hidden: %v", err)
	}
	if data, err := readInRoot(scratch.path, "retained"); err != nil || string(data) != "owned data" {
		t.Fatalf("failed cleanup removed owned evidence: %q/%v", data, err)
	}
}

func TestCampaignScratchRejectsRealDirectorySubstitution(t *testing.T) {
	t.Parallel()
	scratch, err := newCampaignScratch(t.TempDir(), "qualitygate-substitution-control-")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.OpenRoot(filepath.Dir(scratch.path))
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(parent)
	if err = scratch.verifyNamespace(parent); err != nil {
		t.Fatal(err)
	}
	if err = scratch.root.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(scratch.path)
	moved := name + "-moved"
	if err = parent.Rename(name, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(scratch.path); removeAll(filepath.Join(filepath.Dir(scratch.path), moved)) })
	if err = parent.Mkdir(name, dirMode); err != nil {
		t.Fatal(err)
	}
	if err = parent.WriteFile(filepath.Join(name, "foreign"), []byte("replacement data"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err = scratch.close(); !errors.Is(err, errGate) {
		t.Fatalf("real substituted identity accepted: %v", err)
	}
	if data, err := parent.ReadFile(filepath.Join(name, "foreign")); err != nil || string(data) != "replacement data" {
		t.Fatalf("unknown replacement changed: %q/%v", data, err)
	}
}
