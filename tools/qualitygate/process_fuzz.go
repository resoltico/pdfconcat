// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	fuzzExitPoll  = 10 * time.Millisecond
	fuzzPipeDrain = 2 * time.Second
)

func requireFuzzPlatform() error { return nil }

// runFuzzProcess never reaps the group leader until all group signaling has finished.
// The unreaped child pins its PID, so cancellation cannot signal a reused process group.
// Go's inheriting workers share this group; deliberately detached processes are unsupported.
func (c *command) runFuzzProcess(ctx context.Context) (result error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fuzz launch canceled: %w", err)
	}

	observer, err := newFuzzExitObserver()
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, observer.release()) }()

	// CommandContext's Cancel can race Process.Wait after reaping. Observation below
	// therefore owns cancellation, and Cmd.Wait is called exactly once at the end.
	process := exec.Command(c.name, c.args...)
	c.configure(process)
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	process.WaitDelay = fuzzPipeDrain
	if err = process.Start(); err != nil {
		return fmt.Errorf("start fuzz runner: %w", err)
	}

	pid := process.Process.Pid

	observed := observer.attach(pid)
	if observed == nil {
		observed = waitFuzzExit(ctx, &observer, pid)
	}

	// Also stop strays after a normal leader exit, before releasing PID authority.
	terminated := drainFuzzGroup(pid, signalFuzzGroup(pid))
	if terminated != nil {
		// A failed group signal remains a failure, even if direct child cleanup works.
		if killErr := process.Process.Kill(); !errors.Is(killErr, os.ErrProcessDone) {
			terminated = errors.Join(terminated, killErr)
		}
	}

	return errors.Join(observed, terminated, process.Wait())
}

// The cleanup deadline is independent of the already-canceled work context.
// Native group inspection must prove every remaining member is terminal before reaping.
func drainFuzzGroup(pid int, signalErr error) error {
	cleanup, cancel := context.WithTimeout(context.Background(), fuzzPipeDrain)
	defer cancel()

	ticker := time.NewTicker(fuzzExitPoll)
	defer ticker.Stop()

	for {
		if err := cleanup.Err(); err != nil {
			return errors.Join(signalErr, fmt.Errorf("owned fuzz group cleanup expired: %w", err))
		}

		terminal, err := fuzzGroupTerminal(pid, signalErr)
		if err != nil {
			return err
		}

		if err = cleanup.Err(); err != nil {
			return errors.Join(signalErr, fmt.Errorf("owned fuzz group cleanup expired: %w", err))
		}

		if terminal {
			return nil
		}

		select {
		case <-cleanup.Done():
			return errors.Join(signalErr, fmt.Errorf("owned fuzz group did not terminate: %w", cleanup.Err()))
		case <-ticker.C:
		}
	}
}

func waitFuzzExit(ctx context.Context, observer *fuzzExitObserver, pid int) error {
	ticker := time.NewTicker(fuzzExitPoll)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("fuzz execution canceled: %w", err)
		}

		exited, err := observer.exited(pid)
		if err != nil || exited {
			return err
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("fuzz execution canceled: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
