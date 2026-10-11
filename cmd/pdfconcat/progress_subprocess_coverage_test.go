// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

const progressVerboseHelperArgument = "-test.v"

const progressCoverageMetadataChild = "TestProgressCoverageMetadataChild"

var prepareProgressCoverageMetadata = sync.OnceValue(func() error {
	const budget = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate covered test executable: %w", err)
	}

	command := exec.CommandContext(ctx, executable,
		"-test.run=^"+progressCoverageMetadataChild+"$",
		progressVerboseHelperArgument,
		"-test.gocoverdir="+os.Getenv(exectest.EnvCoverDir))
	command.WaitDelay = time.Second
	output, runErr := command.CombinedOutput()
	if ctx.Err() != nil || runErr != nil || !bytes.Contains(output, []byte("--- PASS: "+progressCoverageMetadataChild)) {
		return fmt.Errorf("covered metadata child: context=%v process=%v output=%s", ctx.Err(), runErr, output)
	}

	return nil
})

// The pinned Go test harness emits binary coverage at teardown when its
// -test.gocoverdir flag is forwarded; GOCOVERDIR alone does not collect test helpers.
// A joined selected child writes canonical metadata before parallel children
// start, so their temporary metadata files cannot collide on first emission.
func progressHelperArgs(args ...string) []string {
	directory := os.Getenv(exectest.EnvCoverDir)
	if directory != "" {
		if testing.CoverMode() != "" {
			if err := prepareProgressCoverageMetadata(); err != nil {
				panic("prepare private child coverage metadata: " + err.Error())
			}
		}
		args = append(args, "-test.gocoverdir="+directory)
	}

	return args
}

// This selected child has no product work; its normal Go test exit emits the
// binary metadata that sibling native controls share.
func TestProgressCoverageMetadataChild(_ *testing.T) {}
