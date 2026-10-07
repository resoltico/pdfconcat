// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package plan_test

import (
	"fmt"
	"os/exec"
	"syscall"
	"testing"
)

func prepareInterruptedProcess(_ *exec.Cmd) {}

func interruptReaderProcess(_ *testing.T, command *exec.Cmd) error {
	if err := command.Process.Signal(syscall.SIGINT); err != nil {
		return fmt.Errorf("send native interrupt to decoder fixture: %w", err)
	}

	return nil
}
