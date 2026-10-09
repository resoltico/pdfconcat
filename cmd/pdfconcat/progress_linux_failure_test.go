// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	progressLinuxFailureScenario = "PDFCONCAT_NATIVE_LINUX_FAILURE"
	progressLinuxFailureSelector = "-test.run=^TestProgressLinuxFailureHelper$"
	progressLinuxFailureReceipt  = "native Linux failure complete: "
)

func TestProgressLinuxAcquisitionAndWaitFailures(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	for _, scenario := range []string{"proc-open", "owned-wait"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			command := exec.CommandContext(
				ctx,
				executable,
				progressHelperArgs(progressLinuxFailureSelector, progressVerboseHelperArgument)...)

			command.Env = append(os.Environ(), progressLinuxFailureScenario+"="+scenario)
			output, runErr := command.CombinedOutput()
			t.Logf("actual Linux failure %s: %s", scenario, output)

			if ctx.Err() != nil || runErr != nil || !strings.Contains(string(output), progressLinuxFailureReceipt+scenario) ||
				!strings.Contains(string(output), "--- PASS: TestProgressLinuxFailureHelper") {
				t.Fatalf("native failure not proved (watchdog=%v): %v", ctx.Err(), runErr)
			}
		})
	}
}

func TestProgressLinuxFailureHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressLinuxFailureScenario)
	if scenario == "" {
		return
	}

	switch scenario {
	case "proc-open":
		assertProgressProcAcquisitionFailure(t)
	case "owned-wait":
		assertProgressOwnedWaitFailure(t)
	default:
		t.Fatal("unknown Linux failure scenario")
	}

	t.Log(progressLinuxFailureReceipt + scenario)
}
