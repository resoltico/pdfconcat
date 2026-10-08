// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan_test

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

// A private console prevents the native control event from reaching this test or a user's console.
func prepareInterruptedProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
}

func interruptReaderProcess(t *testing.T, command *exec.Cmd) error {
	t.Helper()
	sender := exectest.Command(t.Context(), exectest.Build(t, fixturePackage), "interrupt-console", strconv.Itoa(command.Process.Pid))

	output, err := sender.CombinedOutput()
	if err != nil {
		return fmt.Errorf("native Windows private-console interrupt prerequisite: %w: %s", err, output)
	}

	return nil
}
