// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFuzzDiscoveryCaptureHelper(t *testing.T) {
	t.Parallel()
	mode := os.Getenv("PDFCONCAT_FUZZ_DISCOVERY_CAPTURE_MODE")
	if mode == "" {
		return
	}
	if mode == "flood" {
		directory, err := os.OpenRoot(os.Getenv("PDFCONCAT_FUZZ_DISCOVERY_CAPTURE_DIR"))
		if err != nil {
			t.Fatal(err)
		}
		defer closeLogged(directory)
		if err := directory.WriteFile("child.pid", []byte(strconv.Itoa(os.Getpid())), fileMode); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprint(os.Stdout, strings.Repeat("x", 32<<10)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
	}
	if _, err := fmt.Fprint(os.Stdout, "discovery stdout\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(os.Stderr, "discovery stderr\n"); err != nil {
		t.Fatal(err)
	}
	if mode == "fail" {
		t.Fatal("intentional discovery failure")
	}
}

func TestBoundedFuzzDiscoveryRetainsExactPrefixAndJoinsNoisyChild(t *testing.T) {
	// Temporary evidence goes outside the disposable directory configured in
	// the child environment, matching the production discovery ownership split.
	durable, disposable := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", durable)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	run := &command{
		name: executable, args: []string{"-test.run=^TestFuzzDiscoveryCaptureHelper$"}, fuzzLifecycle: true,
		env: []string{"PDFCONCAT_FUZZ_DISCOVERY_CAPTURE_MODE=flood", "PDFCONCAT_FUZZ_DISCOVERY_CAPTURE_DIR=" + disposable, "TMPDIR=" + disposable},
	}
	output, runErr := run.boundedFuzzOutput(ctx, 1024)
	if !errors.Is(runErr, errGate) || !strings.Contains(runErr.Error(), "partial fuzz discovery stdout:") || ctx.Err() != nil || len(output) != 1024 {
		t.Fatalf("discovery did not fail with bounded durable evidence before deadline: %d/%v/%v", len(output), runErr, ctx.Err())
	}
	pidBytes, err := readInRoot(disposable, "child.pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	requireFuzzWorkerExited(t, pid)
	files, err := filepath.Glob(filepath.Join(durable, "qualitygate-test-events-*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("partial output was not outside disposable child storage: %v/%v", files, err)
	}
	retained, err := readInRoot(durable, filepath.Base(files[0]))
	if err != nil || string(retained) != output {
		t.Fatalf("retained discovery prefix differs from bounded output: %d/%v", len(retained), err)
	}
	stderr, err := readInRoot(durable, filepath.Base(files[0])+".stderr")
	if err != nil || len(retained)+len(stderr) != 1024 {
		t.Fatalf("combined discovery files exceeded budget: %d/%d/%v", len(retained), len(stderr), err)
	}
}

func TestCommandOutputPreservesOrdinaryAndBoundedPassFailureContracts(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, bounded := range []bool{false, true} {
		for _, mode := range []string{"pass", "fail"} {
			run := &command{
				name: executable, args: []string{"-test.run=^TestFuzzDiscoveryCaptureHelper$"},
				fuzzLifecycle: bounded, env: []string{"PDFCONCAT_FUZZ_DISCOVERY_CAPTURE_MODE=" + mode},
			}
			output, runErr := run.output(t.Context())
			if !strings.Contains(output, "discovery stdout") || strings.Contains(output, "discovery stderr") {
				t.Fatalf("stdout contract changed: %v/%s/%q", bounded, mode, output)
			}
			if mode == "pass" && runErr != nil || mode == "fail" && (runErr == nil || !strings.Contains(runErr.Error(), "discovery stderr")) {
				t.Fatalf("failure stderr contract changed: %v/%s/%v", bounded, mode, runErr)
			}
		}
	}
}
