// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

const progressNativeVerbose = "-test.v"

func assertProgressNativeChild(ctx context.Context, t *testing.T, command *exec.Cmd, receipt, helper string) {
	t.Helper()

	output, runErr := command.CombinedOutput()
	t.Logf("private native boundary: %s", output)

	if ctx.Err() != nil || runErr != nil || !strings.Contains(string(output), receipt) ||
		!strings.Contains(string(output), "--- PASS: "+helper) {
		t.Fatalf("native boundary not proved (watchdog=%v): %v", ctx.Err(), runErr)
	}
}
