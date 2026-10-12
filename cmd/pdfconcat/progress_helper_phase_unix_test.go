//go:build darwin || linux

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Coverage metadata preparation has its own joined five-second bound. It must not consume the
// tested operation's deadline. The process watchdog separately bounds startup, cleanup and exit.
func runClosedStderrChild(t *testing.T, stall string, operationBudget, processBudget time.Duration) (string, error) {
	t.Helper()
	return runProgressPhaseChild(t, "TestProgressClosedCallerStderrHelper", []string{progressOwnedCloseFault + "=1", "PDFCONCAT_CLOSED_STDERR_STALL=" + stall}, operationBudget, processBudget)
}

func runProgressPhaseChild(t *testing.T, selector string, environment []string, operationBudget, processBudget time.Duration) (string, error) {
	t.Helper()
	started := time.Now()
	args := progressHelperArgs("-test.run=^"+selector+"$", "-test.v")
	metadataTime := time.Since(started)
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer closeProgressResource(t, reader)
	defer closeProgressResource(t, writer)
	output, err := os.Create(filepath.Join(t.TempDir(), "child-output"))
	if err != nil {
		return "", err
	}
	defer closeProgressResource(t, output)
	ctx, cancel := context.WithTimeout(t.Context(), processBudget)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.ExtraFiles = []*os.File{writer}
	command.Stdout = output
	command.Stderr = output
	command.Env = append(os.Environ(), environment...)
	launched := time.Now()
	if err := command.Start(); err != nil {
		return "", err
	}
	var protocolErr error
	phase := "startup"
	phases := ""
	scan := bufio.NewScanner(reader)
	for _, expected := range []string{"ready", "operation", "cleanup"} {
		deadline := launched.Add(processBudget)
		if expected != "ready" {
			deadline = time.Now().Add(operationBudget)
		}
		if err := reader.SetReadDeadline(deadline); err != nil {
			protocolErr = err
			break
		}
		if !scan.Scan() {
			protocolErr = errors.Join(scan.Err(), fmt.Errorf("missing %s phase", expected))
			break
		}
		if scan.Text() != expected {
			protocolErr = fmt.Errorf("expected %s phase, got %q", expected, scan.Text())
			break
		}
		phase = expected
		phases += fmt.Sprintf("%s=%s ", expected, time.Since(launched))
	}
	if protocolErr != nil {
		cancel()
	}
	waitErr := command.Wait() // Required reap, including both deliberate stalls and cancellation.
	trace := phases + fmt.Sprintf("metadata_preparation=%s process=%s last_phase=%s reaped=%t context=%v", metadataTime, time.Since(launched), phase, command.ProcessState != nil, ctx.Err())
	data, readErr := os.ReadFile(output.Name())
	if protocolErr != nil || waitErr != nil {
		trace += fmt.Sprintf(" output=%s", data)
	}
	return trace, errors.Join(protocolErr, waitErr, readErr)
}

// Register first so this receipt is emitted only after every native cleanup/restore callback.
func progressPhaseControl(t *testing.T) *os.File {
	t.Helper()
	control := os.NewFile(3, "native-helper-control")
	if control == nil {
		t.Fatal("missing private phase channel")
	}
	t.Cleanup(func() {
		if _, err := fmt.Fprintln(control, "cleanup"); err != nil {
			t.Error(err)
		}
		closeProgressResource(t, control)
	})
	return control
}
