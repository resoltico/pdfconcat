// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func TestWindowsConsoleInterruptBlockedStdinPreservesOutput(t *testing.T) {
	t.Parallel()
	dir := tempDir(t)
	writePDFsAt(t, filepath.Join(dir, fileOut), "original")
	original, err := os.ReadFile(filepath.Clean(filepath.Join(dir, fileOut)))
	ensure(t, err)
	command := start(t, dir, commandBuild, flagPlan, "-", "-o", fileOut, flagOverwrite, flagReport, shortReportPath)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}

	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr
	input, err := command.StdinPipe()
	ensure(t, err)
	registerConsoleInputClose(t, input)
	ensure(t, command.Start())

	waited := false

	registerConsoleChildReap(t, command, &waited)

	// More than the anonymous pipe capacity: completion proves the CLI is actually reading its stdin.
	wrote := make(chan error, 1)

	go func() {
		_, writeErr := io.WriteString(input, string(bytes.Repeat([]byte(" "), 1<<20))+`{"version":1`)
		wrote <- writeErr
	}()

	select {
	case writeErr := <-wrote:
		ensure(t, writeErr)
	case <-time.After(commandTimeout):
		t.Fatal("Windows CLI did not read partial stdin")
	}

	sender := exectest.Command(
		t.Context(),
		exectest.Build(t, "./internal/plan/testdata/decodecmd"),
		"interrupt-console",
		strconv.Itoa(command.Process.Pid),
	)

	sent, sendErr := sender.CombinedOutput()
	if sendErr != nil {
		t.Fatalf("native Windows private-console prerequisite: %v: %s", sendErr, sent)
	}

	res := outcome(t, command.Wait(), &stdout, &stderr, tempOf(command))
	waited = true

	requireExit(t, res, 130)

	if data, readErr := os.ReadFile(filepath.Clean(filepath.Join(dir, fileOut))); readErr != nil || !bytes.Equal(data, original) {
		t.Fatalf("interrupted output changed: %v", readErr)
	}

	requireSavedAttempt(t, dir, shortReportPath, contractObject(t, res.stdout), statusInterrupted)

	if parsed := summaryOf(t, res); parsed.Publication.Published {
		t.Fatal("interrupted input was published")
	}
}

func registerConsoleChildReap(t *testing.T, command *exec.Cmd, waitOwned *bool) {
	t.Helper()
	t.Cleanup(func() {
		if *waitOwned {
			return
		}

		killErr := command.Process.Kill()
		if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			t.Logf("kill Windows CLI: %v", killErr)
		}

		waitErr := command.Wait()
		if waitErr != nil {
			t.Logf("reap Windows CLI: %v", waitErr)
		}
	})
}

// Cmd.Wait closes StdinPipe; early failure still owns its cleanup here.
func registerConsoleInputClose(t *testing.T, input io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		if closeErr := input.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Error(closeErr)
		}
	})
}
