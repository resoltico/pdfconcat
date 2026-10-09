// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin && cgo && progress_native

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	progressSignalScenario = "PDFCONCAT_NATIVE_WRITE_SIGNAL"
	progressSignalSelector = "-test.run=^TestProgressNativeSignalHelper$"
	progressSignalReceipt  = "native non-restarting write signal complete: "
	progressSignalStimulus = "stimulus"
	progressSignalRetry    = "retry"
)

func TestProgressNativeWriteInterruption(t *testing.T) {
	t.Parallel()

	if testing.CoverMode() != "atomic" {
		t.Fatal("native EINTR verification requires CGO_ENABLED=1 go test -race -covermode=atomic")
	}

	executable, err := os.Executable()
	requireProgressNoError(t, err)

	for _, scenario := range []string{progressSignalStimulus, progressSignalRetry} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			directory := progressSignalCoverDirectory(t, scenario)
			command := exec.CommandContext(ctx, executable, progressSignalSelector, progressNativeVerbose, "-test.gocoverdir="+directory)

			command.Env = append(os.Environ(), progressSignalScenario+"="+scenario)
			assertProgressNativeChild(ctx, t, command, progressSignalReceipt+scenario, "TestProgressNativeSignalHelper")

			if scenario == progressSignalRetry {
				assertProgressNativeRetryCounter(ctx, t, directory)
			}
		})
	}
}

func TestProgressNativeSignalHelper(t *testing.T) {
	t.Parallel()

	scenario := os.Getenv(progressSignalScenario)
	if scenario == "" {
		return
	}

	if scenario != progressSignalStimulus && scenario != progressSignalRetry {
		t.Fatal("unknown private native signal scenario")
	}

	observed := runProgressSignalWrite(t, scenario)
	if scenario == progressSignalStimulus {
		if !errors.Is(observed.writeErr, unix.EINTR) || string(observed.pending) != progressEmptyRecord {
			t.Fatalf("real interrupted write did not preserve the whole pending record: %v", observed.writeErr)
		}
	} else {
		requireProgressNoError(t, observed.writeErr)
	}

	assertProgressSignalBytes(t, scenario, observed)
	t.Log(progressSignalReceipt + scenario)
}
