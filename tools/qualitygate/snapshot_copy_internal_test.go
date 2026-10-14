// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotCopyAggregateBoundaryAndDirectoryEntries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, listing string
		bytes         int64
		entries       int
		wantErr       bool
	}{
		{"exact", "a\x00b\x00", 4, 2, false},
		{"aggregate", "a\x00b\x00", 3, 2, true},
		{"empty entry", "a\x00empty\x00", 4, 1, true},
		{"parent entries", "one/two/empty\x00", 0, 2, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			source, target := t.TempDir(), t.TempDir()
			for name, content := range map[string]string{"a": "abc", "b": "x", "empty": "", "one/two/empty": ""} {
				path := filepath.Join(source, name)
				if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), fileMode); err != nil {
					t.Fatal(err)
				}
			}
			budget := &snapshotCopyBudget{row.bytes, row.entries}
			_, err := copyListed(t.Context(), source, target, row.listing, budget)
			if errors.Is(err, errGate) != row.wantErr {
				t.Fatalf("copy error: %v", err)
			}
			var actualBytes int64
			var actualEntries int
			if err := filepath.WalkDir(target, func(path string, e fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if path == target {
					return nil
				}
				actualEntries++
				info, err := e.Info()
				if err != nil {
					return err
				}
				if info.Mode().IsRegular() {
					actualBytes += info.Size()
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if actualBytes > row.bytes || actualEntries > row.entries {
				t.Fatalf("wrote beyond allowance: %d bytes/%d entries", actualBytes, actualEntries)
			}
			if !row.wantErr {
				if got, err := readInRoot(target, "a"); err != nil || string(got) != "abc" {
					t.Fatalf("exact copy: %q/%v", got, err)
				}
			}
		})
	}
}

func TestSnapshotCopyRejectsSparseOversizeWithoutOutput(t *testing.T) {
	t.Parallel()
	source, target := t.TempDir(), t.TempDir()
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	file, err := root.Create("oversize")
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(file.Truncate(snapshotCopyBytes+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err = copyListed(t.Context(), source, target, "oversize\x00", newSnapshotCopyBudget()); !errors.Is(err, errGate) {
		t.Fatalf("sparse oversize accepted: %v", err)
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("oversize created output: %v/%v", entries, err)
	}
}

type snapshotReadControl struct {
	reader    io.Reader
	afterRead func()
}

func (r *snapshotReadControl) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if r.afterRead != nil {
		r.afterRead()
		r.afterRead = nil
	}
	return n, err
}

func TestSnapshotStreamRejectsAppendedByteWithoutWritingIt(t *testing.T) {
	t.Parallel()
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	if err = root.WriteFile("source", []byte("abc"), fileMode); err != nil {
		t.Fatal(err)
	}
	input, err := root.Open("source")
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(input)
	reader := &snapshotReadControl{reader: input, afterRead: func() {
		file, err := root.OpenFile("source", os.O_WRONLY|os.O_APPEND, fileMode)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.WriteString("x")
		if err = errors.Join(err, file.Close()); err != nil {
			t.Fatal(err)
		}
	}}
	var output bytes.Buffer
	if err = streamSnapshotFile(t.Context(), &output, reader, &snapshotCopyBudget{3, 1}); !errors.Is(err, errGate) || output.String() != "abc" {
		t.Fatalf("appended byte wrote past boundary: %q/%v", output.String(), err)
	}
}

func TestSnapshotStreamCancellationPreventsNextChunkWrite(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &snapshotReadControl{reader: strings.NewReader(strings.Repeat("x", snapshotCopyChunk*2)), afterRead: cancel}
	var output bytes.Buffer
	if err := streamSnapshotFile(ctx, &output, reader, newSnapshotCopyBudget()); !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("canceled chunk was written: %d/%v", output.Len(), err)
	}
}

func TestSnapshotSparseRejectionRemovesPartialSnapshot(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("TMPDIR", parent)
	t.Setenv("TMP", parent)
	t.Setenv("TEMP", parent)
	source := snapshotFixture(t, map[string]string{moduleFileName: snapshotModule, snapshotSourceFile: snapshotPackage})
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	file, err := root.Create("oversize")
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(file.Truncate(snapshotCopyBytes+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := snapshotTree(t.Context(), source); snapshot != "" || !errors.Is(err, errGate) {
		t.Fatalf("oversize snapshot accepted: %q/%v", snapshot, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial snapshot remained: %v/%v", entries, err)
	}
}

func TestSnapshotListingRejectsOutputLimitAndAllowsExactBoundary(t *testing.T) {
	t.Parallel()
	source := snapshotFixture(t, map[string]string{"a": "one", "b": "two"})
	listing, err := listSnapshotFiles(t.Context(), source, 4096)
	if err != nil || listing == "" {
		t.Fatalf("listing fixture: %q/%v", listing, err)
	}
	exact, err := listSnapshotFiles(t.Context(), source, int64(len(listing)))
	if err != nil || exact != listing {
		t.Fatalf("exact listing: %q/%v", exact, err)
	}
	if output, err := listSnapshotFiles(t.Context(), source, int64(len(listing)-1)); output != "" || !errors.Is(err, errGate) {
		t.Fatalf("oversize listing accepted: %q/%v", output, err)
	}
}

func TestSnapshotFSMonitorHook(t *testing.T) {
	path := os.Getenv("PDFCONCAT_SNAPSHOT_FSMONITOR")
	if path == "" {
		return
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(root)
	if err = root.WriteFile("called", []byte("configured helper ran"), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Print("token\x00"); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotListingDisablesConfiguredFSMonitor(t *testing.T) {
	source := snapshotFixture(t, map[string]string{snapshotSourceFile: snapshotPackage})
	marker := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hook := "'" + strings.ReplaceAll(filepath.ToSlash(executable), "'", "'\\''") + "' -test.run=^TestSnapshotFSMonitorHook$"
	environment := []string{"PDFCONCAT_SNAPSHOT_FSMONITOR=" + marker}
	for _, args := range [][]string{{"add", snapshotSourceFile}, {"config", "core.fsmonitor", hook}, {"update-index", "--fsmonitor"}} {
		if output, err := (&command{dir: source, name: gitTool, args: args, env: environment}).output(t.Context()); err != nil {
			t.Fatalf("configure actual monitor: %v/%s", err, output)
		}
	}
	markerRoot, err := os.OpenRoot(marker)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(markerRoot)
	if err = markerRoot.Remove("called"); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	// The same listing without its explicit override must actually invoke this
	// configured helper, so disabling it is exercised at the real Git boundary.
	if _, err := (&command{dir: source, name: gitTool, args: []string{"ls-files", "-z", "--cached", "--others", "--exclude-standard"}, env: environment}).output(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := readInRoot(marker, "called"); err != nil {
		t.Fatalf("configured helper did not run in negative control: %v", err)
	}
	if err = markerRoot.Remove("called"); err != nil {
		t.Fatal(err)
	}
	// No environment injection is necessary: an accidental invocation would run
	// the configured test helper and its independent marker environment below.
	t.Setenv("PDFCONCAT_SNAPSHOT_FSMONITOR", marker)
	if _, err = listSnapshotFiles(t.Context(), source, snapshotListingBytes); err != nil {
		t.Fatal(err)
	}
	if _, err = markerRoot.Stat("called"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot launched configured monitor: %v", err)
	}
}

func TestSnapshotCopyPreservesExistingOverlayAndNewFileModes(t *testing.T) {
	t.Parallel()
	source, target := t.TempDir(), t.TempDir()
	for _, name := range []string{"existing", "created"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("new"), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(sourceRoot)
	for _, name := range []string{"existing", "created"} {
		if err = sourceRoot.Chmod(name, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "existing"), []byte("previous longer content"), fileMode); err != nil {
		t.Fatal(err)
	}
	prior, err := os.Stat(filepath.Join(target, "existing"))
	if err != nil {
		t.Fatal(err)
	}
	if copied, err := copyListed(t.Context(), source, target, "existing\x00created\x00", &snapshotCopyBudget{6, 1}); err != nil || copied != 2 {
		t.Fatalf("overlay replacement: %d/%v", copied, err)
	}
	for _, name := range []string{"existing", "created"} {
		if data, err := readInRoot(target, name); err != nil || string(data) != "new" {
			t.Fatalf("overlay bytes %s: %q/%v", name, data, err)
		}
	}
	existing, err := os.Stat(filepath.Join(target, "existing"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(filepath.Join(source, "created"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := os.Stat(filepath.Join(target, "created"))
	if err != nil {
		t.Fatal(err)
	}
	if existing.Mode().Perm() != prior.Mode().Perm() || created.Mode().Perm() != original.Mode().Perm() {
		t.Fatalf("file modes changed: existing%v/%v new%v/%v", existing.Mode(), prior.Mode(), created.Mode(), original.Mode())
	}
}

func TestSnapshotComparisonRejectsGrowthMismatchAndCancellation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"exact", "different", "growth", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			original := &snapshotReadControl{reader: strings.NewReader("abc")}
			copied := "abc"
			switch kind {
			case "different":
				copied = "abd"
			case "growth":
				original.reader = strings.NewReader("abcx")
			case "canceled":
				original.afterRead = cancel
			}
			err := compareSnapshotStreams(ctx, original, strings.NewReader(copied), 3)
			if kind == "exact" && err != nil {
				t.Fatal(err)
			}
			if kind != "exact" && err == nil {
				t.Fatalf("%s comparison accepted", kind)
			}
			if kind == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}
