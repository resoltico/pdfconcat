// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/resoltico/pdfconcat/internal/repopolicy"
)

func coverageEvidenceFixture(t *testing.T) string {
	t.Helper()

	directory := filepath.Join(t.TempDir(), "coverage")
	if err := os.Mkdir(directory, dirMode); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(directory, "unit.out"), []byte("partial coverage evidence"), fileMode); err != nil {
		t.Fatal(err)
	}

	return directory
}

func TestCoverageEvidenceRetainsFailedCollection(t *testing.T) {
	t.Parallel()

	directory := coverageEvidenceFixture(t)
	if err := finishCoverageEvidence(t.Context(), directory, directory, nil, errGate); !errors.Is(err, errGate) {
		t.Fatalf("original result lost: %v", err)
	}

	content, err := readInRoot(directory, "unit.out")
	if err != nil || string(content) != "partial coverage evidence" {
		t.Fatalf("failed evidence lost: %q/%v", content, err)
	}
}

func TestCoverageEvidenceRemovesSuccessfulCollection(t *testing.T) {
	t.Parallel()

	directory := coverageEvidenceFixture(t)
	if err := finishCoverageEvidence(t.Context(), directory, directory, nil, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful scratch retained: %v", err)
	}
}

func TestCoverageEvidencePreservesMergedProfileDespiteReportFailure(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	profile, err := repopolicy.ParseCoverageProfile(strings.NewReader("mode: atomic\nexample.invalid/app/main.go:1.1,2.1 1 0\n"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	failure := errGate

	result := finishCoverageEvidence(ctx, directory, directory, profile, failure)
	if !errors.Is(result, failure) || !errors.Is(result, context.Canceled) {
		t.Fatalf("original and report failures not preserved: %v", result)
	}

	retained, err := readProfile(filepath.Join(directory, "merged.out"))
	if err != nil || len(retained.Blocks) != 1 || retained.Blocks[0].Count != 0 {
		t.Fatalf("failed merged profile lost: %+v/%v", retained, err)
	}
}

func TestCoverageEvidenceWritesFailedMergedProfileAndFunctionReport(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	profile, err := repopolicy.ParseCoverageProfile(strings.NewReader(
		"mode: atomic\ngithub.com/resoltico/pdfconcat/tools/qualitygate/coverage.go:74.77,76.3 1 0\n"))
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()

	err = finishCoverageEvidence(t.Context(), root, directory, profile, errGate)
	if !errors.Is(err, errGate) || err.Error() != errGate.Error() {
		t.Fatalf("unexpected report failure: %v", err)
	}

	for _, name := range []string{"merged.out", "merged.out.func.txt"} {
		content, readErr := readInRoot(directory, name)
		if readErr != nil || len(content) == 0 {
			t.Fatalf("missing retained %s: %q/%v", name, content, readErr)
		}
	}
}
