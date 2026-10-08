// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"time"
)

const (
	mutationCleanupReserve = time.Minute

	// Go WaitDelay leaves 15 seconds of the reserve for final controller cleanup and receipts.
	mutationGracePeriod = 45 * time.Second
)

// One work deadline includes setup, discovery and execution; cleanup fits inside the total budget.
func mutationContext(parent context.Context, started time.Time, duration time.Duration) (context.Context, context.CancelFunc) {
	if duration == 0 {
		return context.WithCancel(parent)
	}

	return context.WithDeadline(parent, started.Add(duration-mutationCleanupReserve))
}

func mutationDurationArgument(ctx context.Context, duration time.Duration) ([]string, error) {
	if duration == 0 {
		return nil, nil
	}

	deadline, present := ctx.Deadline()

	remaining := time.Until(deadline)
	if !present || remaining <= 0 {
		return nil, fmt.Errorf("%w: mutation execution budget exhausted before launch", errGate)
	}

	return []string{"--max-duration", remaining.String()}, nil
}

// runMutation gives the tool its internal deadline before the controller's forced-cleanup backstop.
// Direct file streams avoid a promise that an arbitrary caller-provided writer can be interrupted.
func (c *command) runMutation(ctx context.Context, duration time.Duration) error {
	if canceled := ctx.Err(); canceled != nil {
		return fmt.Errorf("mutation launch canceled: %w", canceled)
	}

	stdout, stdoutOK := c.stdout.(*os.File)

	stderr, stderrOK := c.stderr.(*os.File)
	if !stdoutOK || !stderrOK || stdout == nil || stderr == nil {
		return fmt.Errorf("%w: mutation launcher requires direct file output streams", errGate)
	}

	remaining, budgetErr := mutationDurationArgument(ctx, duration)
	if budgetErr != nil {
		return budgetErr
	}

	args := slices.Concat(c.args, remaining)
	process := exec.CommandContext(ctx, c.name, args...)
	process.Dir = c.dir
	process.Env = append(os.Environ(), c.env...)
	process.Stdout, process.Stderr = stdout, stderr
	process.WaitDelay = mutationGracePeriod
	process.Cancel = func() error {
		if runtime.GOOS == windowsOS {
			return fmt.Errorf("mutation cancellation cannot forward Interrupt on Windows: %w", errors.ErrUnsupported)
		}

		signalErr := process.Process.Signal(os.Interrupt)
		if errors.Is(signalErr, os.ErrProcessDone) {
			return os.ErrProcessDone
		}

		if signalErr != nil {
			return fmt.Errorf("interrupt mutation tool: %w", signalErr)
		}

		return nil
	}

	if runErr := process.Run(); runErr != nil {
		return fmt.Errorf("mutation tool execution: %w", errors.Join(runErr, ctx.Err()))
	}

	return nil
}
