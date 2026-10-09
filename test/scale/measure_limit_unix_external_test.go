// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/test/scale"
)

func descriptorLimitError(err error) bool { return errors.Is(err, unix.EMFILE) }

func descriptorFactsHelper() int {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return helperExitOpenFailed
	}

	raised := limit
	raised.Max++

	err := unix.Setrlimit(unix.RLIMIT_NOFILE, &raised)
	if !errors.Is(err, unix.EPERM) {
		return helperExitOpenFailed
	}

	for fd := range 3 {
		if _, err = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
			return helperExitOpenFailed
		}
	}

	input := make([]byte, 64)

	count, err := unix.Read(unix.Stdin, input)
	if err != nil || string(input[:count]) != "stdio launch witness\n" {
		return helperExitOpenFailed
	}

	output := fmt.Sprintf("limits=%d/%d raise=EPERM pid=%d go=%s stdio=preserved\n", limit.Cur, limit.Max, os.Getpid(), runtime.Version())
	if n, writeErr := unix.Write(unix.Stdout, []byte(output)); writeErr != nil || n != len(output) {
		return helperExitWriteFailed
	}

	if err = scale.WriteFile(os.Getenv(envHelper+"_READY"), nil); err != nil {
		return helperExitWriteFailed
	}

	release := os.Getenv(envHelper + "_RELEASE")

	return awaitDescriptorFixtureRelease(release)
}

func awaitDescriptorFixtureRelease(release string) int {
	var err error
	for {
		_, err = scale.FileSize(release)
		if err == nil {
			return 0
		}

		if !errors.Is(err, fs.ErrNotExist) {
			return helperExitHoldFailed
		}

		time.Sleep(time.Millisecond)
	}
}

func TestMeasuredGoChildRetainsVerifiedKernelDescriptorCeiling(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	dir := t.TempDir()
	ready, release := filepath.Join(dir, "ready"), filepath.Join(dir, "release")

	var (
		once       sync.Once
		releaseErr error
	)

	measured, err := scale.Measure(ctx, scale.RunSpec{
		Binary: os.Args[0], Dir: dir, DescriptorLimit: 64, Stdin: bytes.NewBufferString("stdio launch witness\n"),
		Env: append(os.Environ(), envHelper+"=descriptor-facts", envHelper+"_READY="+ready, envHelper+"_RELEASE="+release),
		OnSample: func() {
			if _, readyErr := scale.FileSize(ready); readyErr == nil {
				once.Do(func() { releaseErr = scale.WriteFile(release, nil) })
			}
		},
	})
	if err != nil || releaseErr != nil || measured.ExitCode != 0 || ctx.Err() != nil {
		t.Fatalf("native limit child failed: %+v %v %v", measured, err, releaseErr)
	}

	assertMeasuredDescriptorChild(t, &measured)
}

func assertMeasuredDescriptorChild(t *testing.T, measured *scale.Measurement) {
	t.Helper()

	ceiling := measured.DescriptorCeiling
	if ceiling == nil || ceiling.Soft != 64 || ceiling.Hard != 64 || ceiling.PID <= 0 || !ceiling.ControlCloseOnExec ||
		!ceiling.HygieneConfigured {
		t.Fatalf("actual launch ceiling missing: %+v", ceiling)
	}

	want := fmt.Sprintf("limits=64/64 raise=EPERM pid=%d go=%s stdio=preserved\n", ceiling.PID, runtime.Version())
	if measured.Stdout != want || len(ceiling.BinarySHA256) != 64 || len(ceiling.SourceSHA256) != 64 || len(ceiling.LauncherSHA256) != 64 {
		t.Fatalf("real initialized Go child or source identity contradicted: stdout=%q ceiling=%+v", measured.Stdout, ceiling)
	}

	if strings.Contains(measured.Stderr, "descriptor launcher:") {
		t.Fatal(measured.Stderr)
	}
}
