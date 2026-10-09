// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"os"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

const progressVerboseHelperArgument = "-test.v"

// The pinned Go test harness emits binary coverage at teardown when its
// -test.gocoverdir flag is forwarded; GOCOVERDIR alone does not collect test helpers.
func progressHelperArgs(args ...string) []string {
	directory := os.Getenv(exectest.EnvCoverDir)
	if directory != "" {
		args = append(args, "-test.gocoverdir="+directory)
	}

	return args
}
