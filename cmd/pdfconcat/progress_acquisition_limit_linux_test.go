// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type progressAcquisitionLimitResult struct {
	transport    *progressTransport
	operationErr error
	restoreErr   error
}

var errProgressAcquisitionLimitRestore = errors.New("original descriptor limits not restored")

func assertProgressProcAcquisitionFailure(t *testing.T) {
	t.Helper()
	reader, source := progressPipe(t)
	warm := progressNativeTransport(t, source)
	requireProgressNoError(t, warm.WriteRecord(t.Context(), []byte(progressEmptyRecord)))
	data := make([]byte, len(progressEmptyRecord))
	_, err := io.ReadFull(reader, data)
	requireProgressNoError(t, err)
	requireProgressNoError(t, warm.Close())
	time.Sleep(time.Millisecond)

	before := snapshotProgressLinuxFiles(t)
	result := runProgressAcquisitionLimit(source)

	if result.transport != nil {
		closeProgressResource(t, result.transport)
	}

	requireProgressNoError(t, result.restoreErr)

	if result.transport != nil {
		t.Fatal("descriptor exhaustion returned an owned transport")
	}

	if !errors.Is(result.operationErr, unix.EMFILE) || !strings.Contains(result.operationErr.Error(), "open owned progress sink") {
		t.Fatalf("actual /proc acquisition failure not established: %v", result.operationErr)
	}

	if !maps.Equal(before, snapshotProgressLinuxFiles(t)) {
		t.Fatal("failed acquisition changed existing native descriptor identity or flags")
	}

	_, err = source.WriteString(progressEmptyRecord)
	requireProgressNoError(t, err)
	_, err = io.ReadFull(reader, data)
	requireProgressNoError(t, err)

	if string(data) != progressEmptyRecord {
		t.Fatal("failed acquisition changed original caller bytes")
	}
}

func runProgressAcquisitionLimit(source *os.File) *progressAcquisitionLimitResult {
	result := &progressAcquisitionLimitResult{}

	var original unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &original); err != nil {
		result.restoreErr = fmt.Errorf("read acquisition descriptor limit: %w", err)
		return result
	}

	limited := original

	limited.Cur = 0
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limited); err != nil {
		result.restoreErr = fmt.Errorf("lower acquisition descriptor limit: %w", err)
		return result
	}
	// Restore before reporting, Go resource setup or coverage output on every unwind.
	defer func() {
		result.restoreErr = unix.Setrlimit(unix.RLIMIT_NOFILE, &original)

		var actual unix.Rlimit

		result.restoreErr = errors.Join(result.restoreErr, unix.Getrlimit(unix.RLIMIT_NOFILE, &actual))
		if actual != original {
			result.restoreErr = errors.Join(result.restoreErr, errProgressAcquisitionLimitRestore)
		}
	}()

	result.transport, result.operationErr = newProgressTransport(source)

	return result
}

func snapshotProgressLinuxFiles(t *testing.T) map[int]progressCloseDescriptorState {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	requireProgressNoError(t, err)

	result := map[int]progressCloseDescriptorState{}

	for _, entry := range entries {
		descriptor, parseErr := strconv.Atoi(entry.Name())
		requireProgressNoError(t, parseErr)

		var stat unix.Stat_t

		statErr := unix.Fstat(descriptor, &stat)
		if errors.Is(statErr, unix.EBADF) {
			continue // The directory enumerator may have already closed its own descriptor.
		}

		requireProgressNoError(t, statErr)

		flags, flagErr := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0)
		requireProgressNoError(t, flagErr)

		status, statusErr := unix.FcntlInt(uintptr(descriptor), unix.F_GETFL, 0)
		requireProgressNoError(t, statusErr)

		result[descriptor] = progressCloseDescriptorState{device: stat.Dev, inode: stat.Ino, mode: stat.Mode, flags: flags, status: status}
	}

	return result
}
