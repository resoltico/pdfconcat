// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	evidenceHelperEnv = "PDFCONCAT_TEST_EVIDENCE_HELPER"
	evidencePanicTail = "\nPANIC evidence tail\n"
)

// TestEventEvidenceHelper is a real child process producing an oversized event and stderr/panic text.
func TestEventEvidenceHelper(t *testing.T) {
	t.Parallel()

	if os.Getenv(evidenceHelperEnv) != "1" {
		return
	}

	if _, err := fmt.Fprint(os.Stdout, strings.Repeat("x", bufferMax+1), evidencePanicTail); err != nil {
		t.Fatal(err)
	}

	if _, err := fmt.Fprint(os.Stderr, "complete process stderr\n"); err != nil {
		t.Fatal(err)
	}

	t.Fatal("intentional child failure")
}

func TestEventEvidenceSurvivesMalformedOversizedOutputAndReapsChild(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "events.jsonl")

	evidence, err := openTestEvidence(file)
	if err != nil {
		t.Fatal(err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	run := &command{name: executable, args: []string{"-test.run=^TestEventEvidenceHelper$"}, env: []string{evidenceHelperEnv + "=1"}}

	outcome, runErr := collectTestEvents(ctx, run, evidence)
	if closeErr := evidence.closeFiles(); closeErr != nil {
		t.Fatal(closeErr)
	}

	if runErr == nil || ctx.Err() != nil || len(outcome.streamErrors) == 0 {
		t.Fatalf("child was not reaped with visible parsing failure: %v/%v/%v", runErr, ctx.Err(), outcome.streamErrors)
	}

	assertRawEventEvidence(t, file)
	evidence.finish(runErr)

	if _, err = os.Stat(file); err != nil {
		t.Fatal("failed evidence removed")
	}

	if _, err = openTestEvidence(file); err == nil {
		t.Fatal("existing evidence overwritten")
	}
}

func TestTemporaryEvidenceRemovedOnlyAfterPassingGate(t *testing.T) {
	t.Parallel()

	evidence, err := openTestEvidence("")
	if err != nil {
		t.Fatal(err)
	}

	if err = evidence.closeFiles(); err != nil {
		t.Fatal(err)
	}

	evidence.finish(nil)

	for _, file := range []string{evidence.path, evidence.path + ".stderr"} {
		if _, err = os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("passing temporary evidence retained: %v", err)
		}
	}
}

func assertRawEventEvidence(t *testing.T, file string) {
	t.Helper()

	stdout, err := readInRoot(filepath.Dir(file), filepath.Base(file))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(stdout, []byte(strings.Repeat("x", bufferMax+1)+evidencePanicTail)) {
		t.Fatal("raw oversized event/panic bytes were lost")
	}

	stderr, err := readInRoot(filepath.Dir(file), filepath.Base(file)+".stderr")
	if err != nil || string(stderr) != "complete process stderr\n" {
		t.Fatalf("stderr evidence: %q/%v", stderr, err)
	}
}

func TestEventEvidenceWriteFailureFailsAndReapsChild(t *testing.T) {
	t.Parallel()

	evidence, err := openTestEvidence("")
	if err != nil {
		t.Fatal(err)
	}

	if err = evidence.closeFiles(); err != nil {
		t.Fatal(err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	run := &command{name: executable, args: []string{"-test.run=^TestEventEvidenceHelper$"}}

	_, runErr := collectTestEvents(ctx, run, evidence)
	if runErr == nil || ctx.Err() != nil {
		t.Fatalf("failed evidence writer was accepted or child hung: %v/%v", runErr, ctx.Err())
	}

	removeAll(evidence.path)
	removeAll(evidence.path + ".stderr")
}
