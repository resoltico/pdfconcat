// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"

	"golang.org/x/sys/unix"
)

type progressPollLimitResult struct {
	writeErr   error
	restoreErr error
}

func fillProgressNativeSink(t *testing.T, descriptor int) int {
	t.Helper()

	written := 0

	// Finish with single-byte writes so a small progress record cannot fit.
	for _, record := range [][]byte{make([]byte, 4096), {0}} {
		filled := false

		for range 65536 {
			count, err := unix.Write(descriptor, record)
			if errors.Is(err, unix.EAGAIN) {
				filled = true
				break
			}

			requireProgressNoError(t, err)

			if count <= 0 || count > len(record) {
				t.Fatal("native fixture write made no bounded progress")
			}

			written += count
		}

		if !filled {
			t.Fatal("native sink fixture did not reach actual EAGAIN")
		}
	}

	return written
}

func runProgressPollLimit(ctx context.Context, transport *progressTransport) *progressPollLimitResult {
	result := &progressPollLimitResult{}

	var original unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &original); err != nil {
		result.restoreErr = fmt.Errorf("read original poll descriptor limit: %w", err)
		return result
	}

	limited := original
	limited.Cur = 0

	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limited); err != nil {
		result.restoreErr = fmt.Errorf("lower poll descriptor limit: %w", err)
		return result
	}

	// Always restore before test reporting, coverage emission or opening resources.
	defer func() {
		result.restoreErr = unix.Setrlimit(unix.RLIMIT_NOFILE, &original)

		var restored unix.Rlimit

		result.restoreErr = errors.Join(result.restoreErr, unix.Getrlimit(unix.RLIMIT_NOFILE, &restored))
		if restored != original {
			result.restoreErr = errors.Join(result.restoreErr, errProgressDescriptorFixture)
		}
	}()

	result.writeErr = transport.WriteRecord(ctx, []byte(progressEmptyRecord))

	return result
}

func progressPollTermios(t *testing.T, descriptor int, scenario string) *unix.Termios {
	t.Helper()

	if scenario != progressPollTerminal {
		return nil
	}

	termios, err := unix.IoctlGetTermios(descriptor, unix.TIOCGETA)
	requireProgressNoError(t, err)

	return termios
}

func assertProgressPollOwnership(t *testing.T, descriptor uintptr, baseline map[int]int, flags int, termios *unix.Termios) {
	t.Helper()

	current, err := progressOpenDescriptorSlots()
	requireProgressNoError(t, err)

	if !maps.Equal(baseline, current) || progressDeliveryFlags(t, descriptor) != flags {
		t.Fatal("native poll failure leaked a descriptor or changed caller flags")
	}

	if termios != nil {
		actual, modeErr := unix.IoctlGetTermios(int(descriptor), unix.TIOCGETA)
		requireProgressNoError(t, modeErr)

		if *actual != *termios {
			t.Fatal("native poll failure changed caller terminal modes")
		}
	}
}
