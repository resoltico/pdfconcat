// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func coverageEvidenceFixture(t *testing.T) (string, *coverageAttempt) {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "attempt")
	attempt, err := startCoverageEvidence(t.Context(), root, directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unit.events.json"), []byte(`{"Action":"fail","Test":"ordinary failure"}`), fileMode); err != nil {
		t.Fatal(err)
	}
	return directory, attempt
}

func readCoverageAttempt(t *testing.T, directory string) coverageAttempt {
	t.Helper()
	data, err := readInRoot(directory, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var attempt coverageAttempt
	if err := json.Unmarshal(data, &attempt); err != nil {
		t.Fatal(err)
	}
	return attempt
}

func TestCoverageFailureKeepsCallerOwnedManifestAndRawEvidence(t *testing.T) {
	t.Parallel()
	directory, attempt := coverageEvidenceFixture(t)
	if err := finishCoverageEvidence(directory, attempt, errGate); !errors.Is(err, errGate) {
		t.Fatalf("original failure lost: %v", err)
	}
	retained := readCoverageAttempt(t, directory)
	if retained.Outcome != "failed" || retained.Complete || retained.Stages["collection"] != "incomplete" || !slices.Contains(retained.Available, "unit.events.json") {
		t.Fatalf("failure evidence mislabeled: %+v", retained)
	}
	if _, err := os.Stat(filepath.Join(directory, "merged.out")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed collection manufactured profile: %v", err)
	}
}

func TestCoverageSuccessRetainsCompleteAttemptAndRejectsReuse(t *testing.T) {
	t.Parallel()
	directory, attempt := coverageEvidenceFixture(t)
	for stage, status := range attempt.Stages {
		if status != "not_applicable" {
			attempt.Stages[stage] = "complete"
		}
	}
	if err := finishCoverageEvidence(directory, attempt, nil); err != nil {
		t.Fatal(err)
	}
	retained := readCoverageAttempt(t, directory)
	if retained.Outcome != "passed" || !retained.Complete {
		t.Fatalf("success not retained: %+v", retained)
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := startCoverageEvidence(t.Context(), root, directory); !errors.Is(err, os.ErrExist) {
		t.Fatalf("attempt reuse accepted: %v", err)
	}
	if reread := readCoverageAttempt(t, directory); reread.Started != retained.Started || reread.Outcome != retained.Outcome {
		t.Fatal("reuse overwrote earlier evidence")
	}
}

func TestCoverageCancelledFinalizationPreservesCancellationAndPartialFiles(t *testing.T) {
	t.Parallel()
	directory, attempt := coverageEvidenceFixture(t)
	attempt.Stages["collection"] = "complete"
	if err := os.WriteFile(filepath.Join(directory, "merged.out"), []byte("validated partial-stage output"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := finishCoverageEvidence(directory, attempt, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	retained := readCoverageAttempt(t, directory)
	if retained.Complete || retained.Outcome != "failed" || !slices.Contains(retained.Available, "merged.out") {
		t.Fatalf("partial output presented as complete or lost: %+v", retained)
	}
}

func TestCoverageManifestFailureRetainsOriginalResult(t *testing.T) {
	t.Parallel()
	directory, attempt := coverageEvidenceFixture(t)
	path := filepath.Join(directory, "manifest.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, dirMode); err != nil {
		t.Fatal(err)
	}
	err := finishCoverageEvidence(directory, attempt, errGate)
	var native *os.LinkError
	if !errors.Is(err, errGate) || !errors.As(err, &native) || native.Op != "rename" || native.New != path || !errors.Is(err, native.Err) {
		t.Fatalf("original/finalization failures lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "unit.events.json")); err != nil {
		t.Fatal("manifest failure erased raw input")
	}
}
