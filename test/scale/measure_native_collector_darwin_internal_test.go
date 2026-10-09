// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func TestNativeDescriptorCollectorObservesHeldFilesAndBufferGrowth(t *testing.T) {
	t.Parallel()

	sampler, err := prepareProcessSampler(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := sampler.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	holdNativeCollectorFiles(t, 40)

	output, err := exectest.Command(t.Context(), sampler.collector, "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		t.Fatal(err)
	}

	reply, valid := parseDescriptorCollectorReply(string(output), os.Getpid())
	if !valid || reply.nativeError != 0 || reply.count < 40 || reply.growth < 2 {
		t.Fatalf("native known descriptors not observed: %s", output)
	}

	if sampler.identity == nil || sampler.identity.SourceSHA256 == "" || sampler.identity.BinarySHA256 == "" ||
		sampler.identity.CompilerSHA256 == "" {
		t.Fatal("native collector compiler/source identity missing")
	}

	t.Logf("actual native known-file/growth frame: %s", output)
}

func holdNativeCollectorFiles(t *testing.T, count int) {
	t.Helper()

	for range count {
		file, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			if closeErr := file.Close(); closeErr != nil {
				t.Error(closeErr)
			}
		})
	}
}

func TestNativeDescriptorCollectorReportsUnreapedChildESRCH(t *testing.T) {
	t.Parallel()
	fixture := ownCollectorProcess(t, exec.CommandContext(t.Context(), successfulChildExecutable))
	// The owned observer proves termination without reaping the child; query the same kernel PID.
	deadline := time.Now().Add(time.Second)

	for {
		_, exited, err := fixture.sampler.exit.confirm(t.Context(), fixture.command.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}

		if exited {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("native child did not terminate")
		}
	}

	pid := fixture.command.Process.Pid

	queryStarted := time.Now()
	output, err := exectest.Command(t.Context(), fixture.sampler.collector, "-p", strconv.Itoa(pid)).Output()
	queryFinished := time.Now()

	if err != nil {
		t.Fatal(err)
	}

	reply, valid := parseDescriptorCollectorReply(string(output), pid)
	if !valid || reply.nativeError != uint64(unix.ESRCH) || reply.count != 0 {
		t.Fatalf("unreaped native exit not reported: %s", output)
	}

	reading := fixture.sampler.failedNativeDescriptorRead(t.Context(), queryStarted, queryFinished, string(output), reply.nativeError)
	if !reading.terminal || reading.failures[0].NativeCode != uint64(unix.ESRCH) {
		t.Fatalf("actual native ESRCH+owned exit rejected: %+v", reading)
	}

	requireNativeProofIntervals(t, reading, queryStarted, queryFinished)
	fixture.waitSuccessfully(t)
	t.Logf("actual unreaped child ESRCH frame: %s", output)
}

func TestNativeDescriptorErrorCannotBecomeTerminalAfterOwnedExit(t *testing.T) {
	t.Parallel()
	fixture := ownCollectorProcess(t, exec.CommandContext(t.Context(), successfulChildExecutable))
	fixture.waitSuccessfully(t)
	reading := fixture.sampler.failedNativeDescriptorRead(t.Context(), time.Now(), time.Now(), "", uint64(unix.EPERM))
	assertInvalidCollector(t, reading)

	if reading.failures[0].NativeCode != uint64(unix.EPERM) {
		t.Fatal("native errno evidence lost")
	}
}

func TestNativeDescriptorCollectorKilledProcessRemainsInvalid(t *testing.T) {
	t.Parallel()
	sampler := samplerForLiveChild(t)

	file := filepath.Join(t.TempDir(), descriptorCollectorFixtureName)
	if err := os.WriteFile(file, []byte("#!/bin/sh\nkill -KILL $$\n"), executableFixtureMode); err != nil {
		t.Fatal(err)
	}

	sampler.collector = file
	reading := sampler.sample(t.Context())
	assertInvalidCollector(t, reading)

	if reading.failures[0].ExitCode != -1 {
		t.Fatalf("collector signal loss hidden: %+v", reading)
	}
}

func TestNativeDescriptorCollectorPreservesPermissionErrno(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Fatal("native permission boundary requires an unprivileged test runner")
	}

	sampler, err := prepareProcessSampler(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := sampler.release(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	output, err := exectest.Command(t.Context(), sampler.collector, "-p", "1").Output()
	if err != nil {
		t.Fatal(err)
	}

	reply, valid := parseDescriptorCollectorReply(string(output), 1)
	if !valid || reply.nativeError != uint64(unix.EPERM) || reply.count != 0 {
		t.Fatalf("native permission failure not preserved: %s", output)
	}

	t.Logf("actual different-owner kernel permission frame: %s", output)
}

func TestSuccessfulNativeCollectorStderrCannotBecomeTerminalAfterExit(t *testing.T) {
	t.Parallel()
	sampler := samplerForLiveChild(t)
	file := filepath.Join(t.TempDir(), descriptorCollectorFixtureName)

	script := []byte("#!/bin/sh\nkill -TERM \"$2\"\nprintf 'p%s\\nc0\\ne0\\ng0\\n' \"$2\"\nprintf 'collector diagnostic\\n' >&2\n")
	if err := os.WriteFile(file, script, executableFixtureMode); err != nil {
		t.Fatal(err)
	}

	sampler.collector = file
	reading := sampler.sample(t.Context())
	assertInvalidCollector(t, reading)

	if reading.failures[0].Stderr != "collector diagnostic\n" {
		t.Fatalf("successful collector diagnostic lost: %+v", reading)
	}
}

func requireNativeProofIntervals(t *testing.T, reading processSample, started, finished time.Time) {
	t.Helper()

	failure := reading.failures[0]
	if !failure.ReadStarted.Equal(started) || !failure.ReadFinished.Equal(finished) {
		t.Fatal("exit proof rewrote the original native read interval")
	}

	if failure.ProofWaitStarted.Before(finished) || failure.ProofWaitFinished.Before(failure.ProofWaitStarted) {
		t.Fatalf("separate proof interval missing: %+v", failure)
	}
}
