// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

//go:embed testdata/descriptor_zero_child.c
var descriptorZeroChildSource string

func TestNativeDescriptorCollectorAcceptsLiveStoppedZeroDescriptorChild(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	binary := compileZeroDescriptorChild(ctx, t)
	fixture := ownCollectorProcess(t, exec.CommandContext(ctx, binary))

	requireStoppedZeroDescriptorChild(ctx, t, fixture)

	output := observeLiveZeroDescriptors(ctx, t, fixture)
	if resumeErr := unix.Kill(fixture.command.Process.Pid, unix.SIGCONT); resumeErr != nil {
		t.Fatal(resumeErr)
	}

	fixture.waitSuccessfully(t)

	if ctx.Err() != nil {
		t.Fatal("watchdog rescue cannot certify zero-descriptor control")
	}

	t.Logf("actual WUNTRACED-stopped live child native frame: %s", output)
}

func requireStoppedZeroDescriptorChild(ctx context.Context, t *testing.T, fixture *collectorProcessFixture) {
	t.Helper()

	pid := fixture.command.Process.Pid

	var status unix.WaitStatus

	observed, err := unix.Wait4(pid, &status, unix.WUNTRACED, nil)
	// x/sys 0.48 BSD Stopped treats SIGSTOP as continued. Darwin's SDK instead reserves
	// SIGCONT (0x13); this fixed fixture requires the exact native SIGSTOP stop encoding.
	expected := unix.WaitStatus(unix.SIGSTOP<<8 | 0x7f)
	if err != nil || observed != pid || status != expected || ctx.Err() != nil {
		t.Fatalf(
			"actual live stopped child not established: pid %d/%d status %#x/%#x error %v context %v",
			observed,
			pid,
			status,
			expected,
			err,
			ctx.Err(),
		)
	}

	proof, exited, err := fixture.sampler.exit.confirm(ctx, pid)
	if err != nil || exited || proof != "owned_process_exit_not_observed" {
		t.Fatalf("stopped child not alive: %s %v %v", proof, exited, err)
	}
}

func observeLiveZeroDescriptors(ctx context.Context, t *testing.T, fixture *collectorProcessFixture) []byte {
	t.Helper()

	pid := fixture.command.Process.Pid
	requireNativeReadinessIdentity(t, fixture.sampler)

	output, err := exectest.Command(ctx, fixture.sampler.collector, "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatal(err)
	}

	reply, valid := parseDescriptorCollectorReply(string(output), pid)
	if !valid || reply.count != 0 || reply.nativeError != 0 || reply.growth != 0 {
		t.Fatalf("real zero descriptor/errno frame missing: %s", output)
	}

	reading := fixture.sampler.sample(ctx)
	if reading.terminal || len(reading.failures) != 0 || reading.descriptors != 0 || reading.descriptorAt.IsZero() {
		t.Fatalf("live zero native query not preserved: %+v", reading)
	}

	return output
}

func compileZeroDescriptorChild(ctx context.Context, t *testing.T) string {
	t.Helper()

	identity, err := resolveNativeCompiler(ctx)
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()

	source := filepath.Join(directory, "descriptor_zero_child.c")
	if writeErr := os.WriteFile(source, []byte(descriptorZeroChildSource), descriptorSourceMode); writeErr != nil {
		t.Fatal(writeErr)
	}

	binary := filepath.Join(directory, "descriptor-zero-child")

	arguments := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-isysroot", identity.SDK, "-o", binary, source}

	output, err := exectest.Command(ctx, identity.CompilerPath, arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("compile real zero descriptor child: %v: %s", err, output)
	}

	sourceHash, err := fileDigest(source)
	if err != nil {
		t.Fatal(err)
	}

	binaryHash, err := fileDigest(binary)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("zero child compiler identity %+v source SHA%s binary SHA%s", identity, sourceHash, binaryHash)

	return binary
}

func requireNativeReadinessIdentity(t *testing.T, sampler *processSampler) {
	t.Helper()

	identity := sampler.identity

	reply, valid := parseDescriptorCollectorReply(identity.ReadinessFrame, os.Getpid())
	if !valid || reply.nativeError != 0 || identity.ReadinessPID != os.Getpid() {
		t.Fatal("collector readiness did not query the real harness parent")
	}

	if identity.ReadinessStarted.IsZero() || identity.ReadinessFinished.Before(identity.ReadinessStarted) {
		t.Fatal("separate readiness timing missing")
	}
}
