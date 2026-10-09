// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const foreignGrayTest = "TestReadDeviceGrayWritePNG"

// This uses the production acquisition/reconstruction/runner and actual foreign
// test. Missing fixtures cannot pass through mocked readers or skipped setup.
func TestForeignStagingAuthenticFixtureRestoresActualNativeTest(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}

	stage, err := prepareForeignTests(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := stage.cleanup(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	patterns := []string{"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"}
	if err = stage.compilerEquivalent(t.Context(), root, patterns, testOptions{}); err != nil {
		t.Fatal(err)
	}

	exerciseForeignFixtureControls(t, root, stage, patterns)

	if err = stage.verify(t.Context(), root); err != nil {
		t.Fatal(err)
	}

	if _, positiveErr := runForeignGrayFixture(t, root, patterns, stage); positiveErr != nil {
		t.Fatal(positiveErr)
	}

	if err = stage.verify(t.Context(), root); err != nil {
		t.Fatal(err)
	}

	if err = stage.compilerEquivalent(t.Context(), root, patterns, testOptions{}); err != nil {
		t.Fatal(err)
	}
}

func runForeignGrayFixture(t *testing.T, root string, patterns []string, stage *foreignTestStage) (*testOutcome, error) {
	t.Helper()

	options := testOptions{stage: stage, run: "^" + foreignGrayTest + "$", require: []string{foreignGrayTest}, timeout: "30s"}

	outcome, evidence, runErr := executeWithEvidence(t.Context(), root, patterns, options)
	if outcome == nil {
		t.Fatal(runErr)
	}

	judged := errors.Join(judgeTests(outcome, patterns, options, runErr), rejectForeignSkips(outcome, stage.sources))
	evidence.finish(judged)

	return outcome, judged
}

func exerciseForeignFixtureControls(t *testing.T, root string, stage *foreignTestStage, patterns []string) {
	t.Helper()

	var err error

	fixture := filepath.Join(
		stage.roots[filepath.Join(root, "third_party", "pdfcpu", "pdfcpu")],
		"pkg",
		"testdata",
		"resources",
		"DeviceGray.raw",
	)

	authentic, err := readInRoot(filepath.Dir(fixture), filepath.Base(fixture))
	if err != nil {
		t.Fatal(err)
	}

	if err = os.Remove(fixture); err != nil {
		t.Fatal(err)
	}

	if err = stage.verify(t.Context(), root); err == nil {
		t.Fatal("omitted fixture escaped staged identity")
	}

	failed, negativeErr := runForeignGrayFixture(t, root, patterns, stage)
	if negativeErr == nil || len(failed.failed) == 0 {
		t.Fatal("missing authentic fixture did not fail actual native test")
	}

	writeForeignGraphFixture(t, fixture, string(authentic))
	changed := append([]byte(nil), authentic...)
	changed[0] ^= 1
	writeForeignGraphFixture(t, fixture, string(changed))

	if err = stage.verify(t.Context(), root); err == nil {
		t.Fatal("same-size fixture change escaped staged identity")
	}

	writeForeignGraphFixture(t, fixture, string(authentic))
}
