// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package scale

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// sampleInterval is the minimum pause between readings. macOS additionally pays for each lsof call.
const (
	sampleInterval = 2 * time.Millisecond
	kibibyte       = 1024
)

func startProcess(command *exec.Cmd, descriptorLimit uint64) error {
	if descriptorLimit != 0 {
		command.Args = append([]string{
			"sh", "-c", `limit=$1; shift; ulimit -n "$limit" && exec "$@"`, "sh", strconv.FormatUint(descriptorLimit, 10), command.Path,
		}, command.Args[1:]...)

		shell, err := exec.LookPath("sh")
		if err != nil {
			return fmt.Errorf("find a shell to set the descriptor limit: %w", err)
		}

		command.Path = shell
	}

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

	if runtime.GOOS == "darwin" {
		return usage.Maxrss
	}

	return usage.Maxrss * kibibyte
}
