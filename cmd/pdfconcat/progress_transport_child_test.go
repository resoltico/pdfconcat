// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin || linux || windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	progressTransportChild       = "PDFCONCAT_PROGRESS_TRANSPORT_CHILD"
	progressTransportReceipt     = "PDFCONCAT_PROGRESS_TRANSPORT_RECEIPT"
	progressTransportEntered     = "native transport entered: "
	progressTransportCleaned     = "native transport cleanup complete: "
	progressTransportFailed      = "native transport cleanup failed: "
	progressTransportChildBudget = 5 * time.Second
)

// These selected bodies use Go/native threads in one joined child process.
// Keep outer process-group authority; this is not arbitrary descendant containment.
// Parent wrappers run before t.Parallel, so an unknown child result can preserve
// an incomplete parent lifecycle without leaving other parallel fixtures active.
func runProgressTransportChild(t *testing.T, budget time.Duration, extraFiles ...*os.File) bool {
	t.Helper()
	if os.Getenv(progressTransportChild) == t.Name() {
		// First registered, last executed: a body-end message cannot prove cleanup.
		t.Cleanup(func() {
			outcome := progressTransportCleaned
			if t.Failed() {
				outcome = progressTransportFailed
			}
			data := []byte(outcome + t.Name() + "\n")
			if err := os.WriteFile(os.Getenv(progressTransportReceipt), data, 0o600); err != nil {
				t.Error(err)
				return
			}
			if _, err := fmt.Print(string(data)); err != nil {
				os.Exit(2)
			}
		})
		if _, err := fmt.Println(progressTransportEntered + t.Name()); err != nil {
			os.Exit(2)
		}
		return false
	}
	executable, err := os.Executable()
	requireProgressNoError(t, err)
	directory := t.TempDir()
	receipt := filepath.Join(directory, "cleanup")
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	args := progressHelperArgs("-test.run=^"+regexp.QuoteMeta(t.Name())+"$", progressVerboseHelperArgument, "-test.timeout="+(budget+time.Second).String())
	command := exec.CommandContext(ctx, executable, args...)
	command.ExtraFiles = extraFiles
	// Preserve the outer command's group; no detached private group or registry.
	command.WaitDelay = time.Second
	var watchdogKilled atomic.Bool
	command.Cancel = func() error {
		err := command.Process.Kill()
		if err == nil {
			watchdogKilled.Store(true)
		}
		return err
	}
	command.Env = append(os.Environ(), progressTransportChild+"="+t.Name(), progressTransportReceipt+"="+receipt)
	reader, writer, pipeErr := os.Pipe()
	requireProgressNoError(t, pipeErr)
	command.Stdout, command.Stderr = writer, writer
	var captured bytes.Buffer
	copied := make(chan error, 1)
	go func() { _, err := io.Copy(&captured, reader); copied <- err }()
	runErr := command.Run() // Wait joins the child; output EOF has independent proof below.
	contextErr := ctx.Err() // Forwarding diagnostics must not change the observed outcome.
	stopped := false
	if command.ProcessState != nil && watchdogKilled.Load() {
		if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); ok {
			stopped = status.Signaled() && status.Signal() == syscall.SIGKILL
			if runtime.GOOS == "windows" {
				stopped = status.ExitStatus() == 1
			}
		}
	}
	captureErr := writer.Close()
	select {
	case err := <-copied:
		captureErr = errors.Join(captureErr, err, reader.Close())
	case <-time.After(time.Second):
		captureErr = errors.Join(captureErr, errors.New("native child capture did not reach EOF"), reader.Close())
		captureErr = errors.Join(captureErr, <-copied) // Closing the owned reader joins the copier.
	}
	output := captured.Bytes()                           // Immutable after the copier joins.
	if _, err := fmt.Print(string(output)); err != nil { // Forward captured diagnostics without t.Log indentation.
		if err := os.RemoveAll(directory); err != nil {
			// Output is unavailable; cleanup failure also preserves the unknown exit.
			os.Exit(2)
		}
		os.Exit(2)
	}
	entered := bytes.Contains(output, []byte(progressTransportEntered+t.Name()+"\n"))
	if errors.Is(contextErr, context.DeadlineExceeded) && entered && stopped && captureErr == nil {
		t.Fatalf("native transport body or cleanup exceeded %s: %v", budget, runErr)
	}
	data, readErr := os.ReadFile(receipt)
	if contextErr == nil && runErr == nil && captureErr == nil {
		complete := []byte(progressTransportCleaned + t.Name() + "\n")
		marker := bytes.Index(output, complete)
		passed := bytes.Index(output, []byte("--- PASS: "+t.Name()+" "))
		if readErr == nil && bytes.Equal(data, complete) && marker >= 0 && passed > marker {
			return true
		}
	}
	var exit *exec.ExitError
	if contextErr == nil && captureErr == nil && errors.As(runErr, &exit) && exit.ExitCode() == 1 && readErr == nil && bytes.Equal(data, []byte(progressTransportFailed+t.Name()+"\n")) && bytes.Contains(output, []byte("--- FAIL: "+t.Name()+" ")) {
		t.Fatalf("native transport assertion or cleanup failed: %v", runErr)
	}
	// Unknown early exit/fatal is not a named assertion failure. Keep the current
	// named parent unfinished; the strict mutation judgment rejects this protocol.
	cleanupErr := os.RemoveAll(directory)
	if _, err := fmt.Printf("unknown native transport child outcome (context=%v process=%v capture=%v receipt cleanup=%v)\n", contextErr, runErr, captureErr, cleanupErr); err != nil {
		os.Exit(2)
	}
	os.Exit(2)
	return true
}
