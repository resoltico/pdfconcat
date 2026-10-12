// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

const progressVerboseHelperArgument = "-test.v"

// Every instrumented child owns metadata/counters in one directory. Parallel processes must
// never rename temporary metadata in the same directory; completed directories are merged by the gate.
func progressHelperArgs(args ...string) []string {
	return progressOwnedHelperArgs("progress-child-", args...)
}

func progressOwnedHelperArgs(prefix string, args ...string) []string {
	root := os.Getenv(exectest.EnvCoverDir)
	if root == "" {
		return args
	}
	directory, err := os.MkdirTemp(root, prefix)
	if err != nil {
		panic("create owned child coverage: " + err.Error())
	}
	return append(args, "-test.gocoverdir="+directory)
}

// Deliberately killed/fatal test-protocol controls retain instrumented partial evidence in the
// same caller-owned attempt, separate from the completed child profiles used for measurement.
func progressControlArgs(args ...string) ([]string, string) {
	root := os.Getenv(exectest.EnvCoverDir)
	if root == "" {
		return args, ""
	}
	controls := filepath.Join(filepath.Dir(root), "native-helper-controls")
	attempt, err := os.OpenRoot(filepath.Dir(root))
	if err != nil {
		panic("open control evidence root: " + err.Error())
	}
	createErr := attempt.Mkdir("native-helper-controls", 0o700)
	closeErr := attempt.Close()
	if createErr != nil && !errors.Is(createErr, os.ErrExist) {
		panic("create control evidence: " + createErr.Error())
	}
	if closeErr != nil {
		panic("close control evidence root: " + closeErr.Error())
	}
	directory, err := os.MkdirTemp(controls, "child-")
	if err != nil {
		panic("create owned control coverage: " + err.Error())
	}
	return append(args, "-test.gocoverdir="+directory), exectest.EnvCoverDir + "=" + directory
}
