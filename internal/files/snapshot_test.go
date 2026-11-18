// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/core/diag"
)

func TestSnapshotKeepsObservedBytesAndCleansPrivateSpill(t *testing.T) {
	for _, tc := range []struct {
		name   string
		memory int64
	}{
		{"memory", 1024},
		{"spill", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			t.Setenv("TMP", dir)
			path := filepath.Join(dir, "source.pdf")
			original := []byte("original bytes")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			source, err := Acquire(t.Context(), path, tc.memory)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = source.Close() })
			if source.Size() != int64(len(original)) || source.Digest() != sha256.Sum256(original) {
				t.Fatal("incorrect acquisition facts")
			}
			if (source.spillName != "") != (tc.memory == 0) {
				t.Fatal("incorrect snapshot placement")
			}
			if err := os.WriteFile(path, []byte("modified bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				reader, err := source.Open()
				if err != nil {
					t.Fatal(err)
				}
				actual, readErr := io.ReadAll(reader)
				err = errors.Join(readErr, reader.Close())
				if err != nil || !bytes.Equal(actual, original) {
					t.Fatalf("reopened snapshot %q: %v", actual, err)
				}
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
				t.Fatalf("snapshot cleanup touched the original or leaked spill: %v, %v", entries, err)
			}
		})
	}
}

func TestSnapshotReportsUnavailableSpillAndInterruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(path, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")
	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	if source, err := Acquire(t.Context(), path, 0); source != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected private snapshot creation to fail: %v, %v", source, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if source, err := Acquire(ctx, path, 0); source != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not observed before spill creation: %v, %v", source, err)
	}
}

func TestAcceptedSourceIdentityProtectsMovedInput(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "output.pdf")
	if err := os.WriteFile(source, []byte("observed"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Acquire(t.Context(), source, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snapshot.Close() }()
	if err := os.Rename(source, output); err != nil {
		t.Fatal(err)
	}
	roles := Roles{Output: output, Inputs: []string{source}, InputsKnown: true, Overwrite: true}
	if initial := roles.CheckOutput(); len(initial) != 0 {
		t.Fatalf("path-only control unexpectedly refused the move: %v", initial)
	}
	roles.AcceptedSources = []InputIdentity{snapshot.Identity()}
	if checked := roles.CheckOutput(); len(checked) != 1 || checked[0].Code != diag.OutputIsInput {
		t.Fatalf("publication lost the accepted source identity: %v", checked)
	}
}
