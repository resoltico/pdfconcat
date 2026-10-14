// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

func TestMutationPartitionsAreDisjointAndCoverEveryHostFile(t *testing.T) {
	t.Parallel()
	files := map[string]bool{"cmd/pdfconcat/main.go": true, "internal/app/job.go": true, "internal/app/pipeline.go": true, "internal/capture/set.go": true, "internal/plan/decode.go": true}
	sizes := map[string]int{}
	for path := range files {
		sizes[path] = len(path)
	}
	for count := 1; count <= len(files); count++ {
		union := map[string]bool{}
		for index := range count {
			part, err := selectMutationPartition(sizes, mutationPartition{Index: index, Count: count})
			if err != nil || len(part) == 0 {
				t.Fatalf("empty partition: %v", err)
			}
			for path := range part {
				if union[path] {
					t.Fatalf("duplicate file: %s", path)
				}
				union[path] = true
			}
		}
		if !maps.Equal(union, files) {
			t.Fatal("partition union omitted host runtime files")
		}
	}
	if len(files) != 5 {
		t.Fatal("partitioning changed the caller's inventory")
	}
}

func TestMutationPartitionReceiptBindsFullSourceAndRefusesReuse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]bool{"a.go": true, "b.go": true, "c.go": true}
	for path := range files {
		writeForeignGraphFixture(t, filepath.Join(root, path), "source:"+path)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Scope fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Scope fixture"}} {
		if _, err := (&command{dir: root, name: gitTool, args: args}).output(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	reportFile := filepath.Join(t.TempDir(), "report.json")
	options := &mutationOptions{root: root, snapshot: root, partition: mutationPartition{Index: 1, Count: 3}}
	selected, err := partitionMutationScope(t.Context(), scopeOfMutation{hostFiles: files}, options, reportFile)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(selected.hostFiles, map[string]bool{"b.go": true}) {
		t.Fatal("wrong native source selection")
	}
	for _, path := range []string{"a.go", "c.go"} {
		if !slices.Contains(selected.excludes, "^"+regexp.QuoteMeta(path)+"$") {
			t.Fatal("unselected source still reaches discovery")
		}
	}
	data, err := readInRoot(filepath.Dir(reportFile), filepath.Base(reportFile)+".scope.json")
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Commit        string
		FullInventory map[string]string `json:"full_inventory_sha256"`
		Selected      []string          `json:"selected_files"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	commit, err := coverageCommit(t.Context(), root)
	if err != nil || receipt.Commit != commit || len(receipt.FullInventory) != len(files) || !slices.Equal(receipt.Selected, []string{"b.go"}) {
		t.Fatal("receipt lost commit or complete source inventory")
	}
	if _, err := partitionMutationScope(t.Context(), scopeOfMutation{hostFiles: files}, options, reportFile); !errors.Is(err, os.ErrExist) {
		t.Fatalf("receipt overwrite accepted: %v", err)
	}
	reread, err := readInRoot(filepath.Dir(reportFile), filepath.Base(reportFile)+".scope.json")
	if err != nil || string(reread) != string(data) {
		t.Fatal("reuse changed previous receipt")
	}
}

func TestMutationPartitionRejectsInvalidAndEmptySelections(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "1", "-1/3", "3/3", "0/0", "0/-1", "0/2/3", "x/3"} {
		if _, err := parseMutationFlags([]string{"-partition=" + value}); err == nil {
			t.Fatalf("invalid partition accepted: %s", value)
		}
	}
	if _, err := selectMutationPartition(map[string]int{"one.go": 1}, mutationPartition{Count: 2}); err == nil {
		t.Fatal("empty partition accepted")
	}
}

func TestMutationPartitionsBalanceLargeFilesDeterministically(t *testing.T) {
	t.Parallel()
	// Alphabetic round robin would place both large files in partition zero.
	sizes := map[string]int{"a.go": 100, "b.go": 1, "c.go": 100, "d.go": 1}
	for index := range 2 {
		want := map[string]bool{"a.go": true, "b.go": true}
		if index == 1 {
			want = map[string]bool{"c.go": true, "d.go": true}
		}
		for range 20 {
			got, err := selectMutationPartition(sizes, mutationPartition{Index: index, Count: 2})
			if err != nil || !maps.Equal(got, want) {
				t.Fatalf("partition %d = %v, %v; want %v", index, got, err, want)
			}
		}
	}
	for count := 1; count <= 3; count++ {
		for index := range count {
			got, err := selectMutationPartition(map[string]int{"a": 0, "b": 0, "c": 0}, mutationPartition{Index: index, Count: count})
			if err != nil || len(got) == 0 {
				t.Fatalf("zero-byte partition %d/%d: %v, %v", index, count, got, err)
			}
		}
	}
	if _, err := selectMutationPartition(nil, mutationPartition{Count: 1}); err == nil {
		t.Fatal("empty source inventory accepted")
	}
}
