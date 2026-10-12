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
	"time"
)

// These fixtures execute the collector's real Go commands, rather than just its finalizer.
func coverageCollectionFixture(t *testing.T, body string) (string, string, *coverageAttempt) {
	t.Helper()
	root := t.TempDir()
	writeForeignGraphFixture(t, filepath.Join(root, "go.mod"), "module coverage.attempt.fixture\n")
	writeForeignGraphFixture(t, filepath.Join(root, "value.go"), "package fixture\nfunc Value() int { return 1 }\n")
	writeForeignGraphFixture(t, filepath.Join(root, "value_test.go"), body)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Coverage fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "Coverage collection fixture"}} {
		if _, err := (&command{dir: root, name: gitTool, args: args}).output(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	directory := filepath.Join(t.TempDir(), "attempt")
	attempt, err := startCoverageEvidence(t.Context(), root, directory)
	if err != nil {
		t.Fatal(err)
	}
	return root, directory, attempt
}

func TestCoverageCollectorConversionFailureRetainsRealEvidence(t *testing.T) {
	t.Parallel()
	root, directory, attempt := coverageCollectionFixture(t, `package fixture
import ("os"; "path/filepath"; "testing")
func TestValue(t *testing.T) {
 if Value()!=1 { t.Fatal("value") }
 // Deliberately corrupt binary inputs: the actual SDK converter must reject them.
 for _, name := range []string{"covmeta.0123456789abcdef0123456789abcdef", "covcounters.0123456789abcdef0123456789abcdef.1.1"} {
  if err:=os.WriteFile(filepath.Join(os.Getenv("PDFCONCAT_COVERDIR"),name),[]byte("corrupt coverage input"),0600);err!=nil { t.Fatal(err) }
 }
}

`)
	_, err := collectBaselineCoverage(t.Context(), root, directory, []string{"coverage.attempt.fixture"}, attempt)
	if err == nil || !strings.Contains(err.Error(), "convert executable coverage") {
		t.Fatalf("real conversion failure was not preserved: %v", err)
	}
	if finalErr := finishCoverageEvidence(directory, attempt, err); !errors.Is(finalErr, err) {
		t.Fatalf("finalization lost conversion failure: %v", finalErr)
	}
	retained := readCoverageAttempt(t, directory)
	if retained.Complete || retained.Outcome != "failed" || retained.Stages["unit"] != "complete" || retained.Stages["conversion"] != "incomplete" {
		t.Fatalf("failed conversion mislabeled: %+v", retained)
	}
	for _, name := range []string{"unit.events.json", "unit.events.json.stderr", "unit.out", "covdata-conversion.log"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "merged.out")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed conversion manufactured merged output: %v", err)
	}
}

func TestCoverageCollectorCancellationRetainsStartedCollection(t *testing.T) {
	t.Parallel()
	root, directory, attempt := coverageCollectionFixture(t, `package fixture
import ("os"; "testing"; "time")
func TestValue(t *testing.T) {
 if Value()!=1 { t.Fatal("value") }
 if err:=os.WriteFile("collection-started",[]byte("ready"),0600);err!=nil { t.Fatal(err) }
 // Finite private work ensures the fixture itself cannot leave an unbounded child.
 time.Sleep(500*time.Millisecond)
}
`)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := collectBaselineCoverage(ctx, root, directory, []string{"coverage.attempt.fixture"}, attempt)
		done <- err
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "collection-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("collection fixture did not begin")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	err := <-done
	if err == nil {
		t.Fatal("cancelled collection passed")
	}
	if finalErr := finishCoverageEvidence(directory, attempt, err); !errors.Is(finalErr, context.Canceled) {
		t.Fatalf("original cancellation was lost: %v", finalErr)
	}
	retained := readCoverageAttempt(t, directory)
	if retained.Complete || retained.Outcome != "failed" || retained.Stages["unit"] != "incomplete" {
		t.Fatalf("cancelled collection mislabeled: %+v", retained)
	}
	if data, readErr := readInRoot(directory, "unit.events.json"); readErr != nil || !strings.Contains(string(data), "TestValue") {
		t.Fatalf("started test evidence lost: %v", readErr)
	}
}

func TestCoverageCorruptProfileCannotBecomeAuthoritative(t *testing.T) {
	t.Parallel()
	directory, attempt := coverageEvidenceFixture(t)
	path := filepath.Join(directory, "unit.out")
	if err := os.WriteFile(path, []byte("mode: atomic\nfixture/value.go:1.1,1.2 1 invalid\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfile(path); err == nil {
		t.Fatal("corrupt count accepted")
	} else if finalErr := finishCoverageEvidence(directory, attempt, err); !errors.Is(finalErr, err) {
		t.Fatalf("corrupt-profile failure lost: %v", finalErr)
	}
	if retained := readCoverageAttempt(t, directory); retained.Complete || retained.Outcome != "failed" {
		t.Fatalf("corrupt profile became authoritative: %+v", retained)
	}
}
