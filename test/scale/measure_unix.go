// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// sampleInterval is the minimum pause between readings. Darwin additionally launches a native descriptor query per sample.
const (
	sampleInterval = 2 * time.Millisecond
	kibibyte       = 1024
)

func startProcess(command *exec.Cmd) error {
	err := command.Start()
	if err != nil {
		return fmt.Errorf("launch process: %w", err)
	}

	return nil
}

// peakResidentBytes reads the child's peak resident set from its resource usage, which the kernel
// accounts exactly (macOS reports bytes, other systems kilobytes).
func peakResidentBytes(state *os.ProcessState, _ int64) int64 {
	usage, isRusage := state.SysUsage().(*syscall.Rusage)
	if !isRusage {
		return 0
	}

	if runtime.GOOS == darwinPlatform {
		return usage.Maxrss
	}

	return usage.Maxrss * kibibyte
}
