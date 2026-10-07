// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// command describes one child process.
type command struct {
	stdout io.Writer
	stderr io.Writer
	dir    string
	name   string
	env    []string
	args   []string
}

// run executes the command and returns its error; output goes to the configured writers.
func (c *command) run(ctx context.Context) error {
	process := exec.CommandContext(ctx, c.name, c.args...)
	process.Dir = c.dir
	process.Env = append(os.Environ(), c.env...)
	process.Stdout = c.stdout
	process.Stderr = c.stderr

	err := process.Run()
	if err != nil {
		return fmt.Errorf("%s %s: %w", c.name, strings.Join(c.args, " "), err)
	}

	return nil
}

// output runs the command and returns its standard output; a failure carries the standard error text.
func (c *command) output(ctx context.Context) (string, error) {
	var stdout, stderr bytes.Buffer

	captured := *c
	captured.stdout = &stdout
	captured.stderr = &stderr

	err := captured.run(ctx)
	if err != nil {
		return stdout.String(), fmt.Errorf("%w\n%s", err, indent(stderr.String()))
	}

	return stdout.String(), nil
}

// goCommand builds a `go` invocation in dir.
func goCommand(dir string, args ...string) *command {
	return &command{dir: dir, name: goTool, args: args}
}

// findTool returns the path of a development tool: the repository's .tools/bin first, then PATH.
func findTool(root, name string) (string, error) {
	local := filepath.Join(root, ".tools", "bin", name)
	if runtime.GOOS == windowsOS {
		local += exeSuffix
	}

	_, err := os.Stat(local)
	if err == nil {
		return local, nil
	}

	found, lookErr := exec.LookPath(name)
	if lookErr != nil {
		return "", fmt.Errorf("%w: %s not found in .tools/bin or PATH; run `go run ./tools/installtools %s`", errGate, name, name)
	}

	return found, nil
}
