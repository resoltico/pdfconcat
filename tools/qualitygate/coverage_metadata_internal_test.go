// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type coverageMetadataPair struct {
	directory, temporary, canonical string
	original                        []byte
}

func realCoverageMetadataPair(t *testing.T) coverageMetadataPair {
	t.Helper()

	root := t.TempDir()
	binary := buildCoverageBranchFixture(t, root)

	directory := filepath.Join(root, "coverage")
	if err := os.Mkdir(directory, dirMode); err != nil {
		t.Fatal(err)
	}

	runCoverageBranchFixture(t, root, binary, directory, []string{"choose-first"})
	canonical := generatedCoverageFile(t, directory, "covmeta.")

	temporary := filepath.Join(directory, "tmp."+filepath.Base(canonical)+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Rename(canonical, temporary); err != nil {
		t.Fatal(err)
	}

	original, err := readInRoot(directory, filepath.Base(temporary))
	if err != nil {
		t.Fatal(err)
	}

	// Emit canonical metadata independently and execute the other branch; keep every real counter.
	runCoverageBranchFixture(t, root, binary, directory, nil)

	return coverageMetadataPair{directory: directory, temporary: temporary, canonical: canonical, original: original}
}

func generatedCoverageFile(t *testing.T, directory, prefix string) string {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(directory, entry.Name())
		}
	}

	t.Fatalf("real covered program emitted no %s file", prefix)

	return ""
}

func TestExecutableCoverageReconcilesOnlyIdenticalRealMetadata(t *testing.T) {
	t.Parallel()

	pair := realCoverageMetadataPair(t)

	inputs, err := executableCoverageInputs(pair.directory)
	if err != nil || !slices.Equal(inputs, []string{pair.directory}) {
		t.Fatalf("identical real metadata rejected: %v/%v", inputs, err)
	}

	profile := filepath.Join(t.TempDir(), "reconciled.out")

	args := []string{goToolVerb, covdataVerb, "textfmt", "-i=" + strings.Join(inputs, ","), "-o=" + profile}
	if convertErr := goCommand(filepath.Dir(pair.directory), args...).run(t.Context()); convertErr != nil {
		t.Fatal(convertErr)
	}

	assertCoverageBranches(t, profile)

	for _, path := range []string{pair.temporary, pair.canonical} {
		data, readErr := readInRoot(pair.directory, filepath.Base(path))
		if readErr != nil || !bytes.Equal(data, pair.original) {
			t.Fatalf("raw metadata changed during reconciliation: %s/%v", path, readErr)
		}
	}
}

func TestExecutableCoverageRejectsUnreconciledRealMetadata(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"mismatched", "truncated", "missing-canonical", "unknown-temporary", "temporary-counters", "zero-timestamp", "overflow-timestamp",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			pair := realCoverageMetadataPair(t)
			invalidateCoverageMetadata(t, scenario, pair)

			if inputs, err := executableCoverageInputs(pair.directory); err == nil {
				t.Fatalf("unreconciled native coverage accepted: %s/%v", scenario, inputs)
			}
		})
	}
}

func invalidateCoverageMetadata(t *testing.T, scenario string, pair coverageMetadataPair) {
	t.Helper()

	var err error

	switch scenario {
	case "mismatched":
		changed := slices.Clone(pair.original)
		changed[0] ^= 1
		err = os.WriteFile(pair.temporary, changed, fileMode)
	case "truncated":
		err = os.Truncate(pair.temporary, int64(len(pair.original)-1))
	case "missing-canonical":
		err = os.Remove(pair.canonical)
	case "unknown-temporary":
		err = os.Rename(pair.temporary, filepath.Join(pair.directory, "tmp.unknown"))
	case "temporary-counters":
		counter := generatedCoverageFile(t, pair.directory, "covcounters.")
		err = os.Rename(counter, filepath.Join(pair.directory, "tmp."+filepath.Base(counter)))
	case "zero-timestamp":
		err = os.Rename(pair.temporary, filepath.Join(pair.directory, "tmp."+filepath.Base(pair.canonical)+"0"))
	case "overflow-timestamp":
		err = os.Rename(pair.temporary, filepath.Join(pair.directory, "tmp."+filepath.Base(pair.canonical)+"9223372036854775808"))
	default:
		t.Fatalf("unknown metadata control: %s", scenario)
	}

	if err != nil {
		t.Fatal(err)
	}
}
