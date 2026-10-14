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

func TestFuzzEvidenceLimitHelper(t *testing.T) {
	t.Parallel()
	pidFile := os.Getenv("PDFCONCAT_FUZZ_EVIDENCE_LIMIT_PID")
	if pidFile == "" {
		return
	}
	directory, err := os.OpenRoot(filepath.Dir(pidFile))
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogged(directory)
	if err := directory.WriteFile(filepath.Base(pidFile), []byte(strconv.Itoa(os.Getpid())), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(os.Stdout, strings.Repeat("x", 64<<10)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Hour)
}

func TestFuzzEvidenceBudgetCancelsAndJoinsRealChildRetainingPartialOutput(t *testing.T) {
	t.Parallel()
	evidence, err := openTestEvidence("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeAll(evidence.path); removeAll(evidence.path + ".stderr") })
	evidence.byteLimit = 1024
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	run := &command{
		name: executable, args: []string{"-test.run=^TestFuzzEvidenceLimitHelper$"},
		env: []string{"PDFCONCAT_FUZZ_EVIDENCE_LIMIT_PID=" + pidFile}, fuzzLifecycle: true,
	}
	_, runErr := collectTestEvents(ctx, run, evidence)
	if closeErr := evidence.closeFiles(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !errors.Is(runErr, errGate) || !strings.Contains(runErr.Error(), "evidence limit") || ctx.Err() != nil {
		t.Fatalf("output limit did not fail independently of outer deadline: %v/%v", runErr, ctx.Err())
	}
	pidBytes, err := readInRoot(filepath.Dir(pidFile), filepath.Base(pidFile))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	requireFuzzWorkerExited(t, pid)
	if err = evidence.finish(runErr); err != nil {
		t.Fatal(err)
	}
	var retained int64
	for _, name := range []string{evidence.path, evidence.path + ".stderr"} {
		info, statErr := os.Stat(name)
		if statErr != nil {
			t.Fatal(statErr)
		}
		retained += info.Size()
	}
	if retained != evidence.byteLimit {
		t.Fatalf("retained %d bytes, want exact bounded prefix %d", retained, evidence.byteLimit)
	}
}
