// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"errors"
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

	// The fixed 16 MiB overflow fixture takes about nine seconds under isolated atomic
	// coverage here; ten seconds confuses instrumentation/load with a drain failure.
	// Retain a finite deadline plus exact raw-byte, parsing-error and joined-child checks.
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
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
	if err = evidence.finish(runErr); err != nil {
		t.Fatal(err)
	}

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

	if err = evidence.finish(nil); err != nil {
		t.Fatal(err)
	}

	for _, file := range []string{evidence.path, evidence.path + ".stderr"} {
		if _, err = os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("passing temporary evidence retained: %v", err)
		}
	}
}

func TestEvidenceCompanionFailureRemovesOnlyNewStdout(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	const retained = "existing stderr evidence"
	if err := os.WriteFile(path+".stderr", []byte(retained), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := openTestEvidence(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("occupied stderr accepted: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty stdout orphan retained: %v", err)
	}
	if data, err := readInRoot(filepath.Dir(path), filepath.Base(path)+".stderr"); err != nil || string(data) != retained {
		t.Fatalf("existing stderr changed: %q/%v", data, err)
	}
}

func TestEvidenceCleanupRejectsNonemptyDirectoryWithoutRemovingData(t *testing.T) {
	t.Parallel()
	evidence, err := openTestEvidence("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(evidence.path) })
	t.Cleanup(func() { removeAll(evidence.path + ".stderr") })
	if err = evidence.closeFiles(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(evidence.path); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(evidence.path, dirMode); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(evidence.path, "retained")
	if err = os.WriteFile(retained, []byte("preserve substituted directory data"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err = evidence.finish(nil); err == nil || !strings.Contains(err.Error(), evidence.path) {
		t.Fatalf("cleanup failure lacks exact path: %v", err)
	}
	if data, readErr := readInRoot(evidence.path, filepath.Base(retained)); readErr != nil || string(data) != "preserve substituted directory data" {
		t.Fatalf("cleanup recursively removed unrelated data: %q/%v", data, readErr)
	}
}

func TestEvidenceKeepsExplicitSuccessAndImplicitFailure(t *testing.T) {
	t.Parallel()
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%t", explicit), func(t *testing.T) {
			t.Parallel()
			requested := ""
			var failure error
			if explicit {
				requested = filepath.Join(t.TempDir(), "events.jsonl")
			} else {
				failure = errors.New("failed target")
			}
			evidence, err := openTestEvidence(requested)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { removeAll(evidence.path) })
			t.Cleanup(func() { removeAll(evidence.path + ".stderr") })
			if _, err = evidence.stdout.WriteString("stdout evidence"); err != nil {
				t.Fatal(err)
			}
			if _, err = evidence.stderr.WriteString("stderr evidence"); err != nil {
				t.Fatal(err)
			}
			if err = evidence.closeFiles(); err != nil {
				t.Fatal(err)
			}
			if err = evidence.finish(failure); err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string]string{evidence.path: "stdout evidence", evidence.path + ".stderr": "stderr evidence"} {
				if data, readErr := readInRoot(filepath.Dir(path), filepath.Base(path)); readErr != nil || string(data) != want {
					t.Fatalf("evidence lost: %s: %q/%v", path, data, readErr)
				}
			}
		})
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
