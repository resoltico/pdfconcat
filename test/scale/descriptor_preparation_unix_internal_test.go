// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale

import (
	"context"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func TestInvalidDescriptorPreparationKillsAndReapsOwnedProcess(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ name, script string }{
		{missingExecutableFixture, "exec 3>&-; exec /bin/sleep 30"},
		{"malformed", "printf '{bad}\n' >&3; exec 3>&-; exec /bin/sleep 30"},
		{"wrong identity", "printf '{}\n' >&3; exec 3>&-; exec /bin/sleep 30"},
		{"oversized", "printf '%5000s\n' x >&3; exec 3>&-; exec /bin/sleep 30"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			measureInvalidPreparation(t, test.script)
		})
	}
}

func measureInvalidPreparation(t *testing.T, script string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	command := exectest.Command(ctx, controlledShellExecutable, "-c", script)

	launch, err := prepareDescriptorLaunch(ctx, command, 64)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if releaseErr := launch.release(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})
	// An actual native producer sends invalid IPC; no decoder/syscall/provider hook supplies it.
	command.Path = controlledShellExecutable
	command.Args = []string{"sh", "-c", script}

	sampler, err := prepareProcessSampler(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	measured, measureErr := measurePreparedCommand(ctx, RunSpec{Binary: controlledShellExecutable}, command, sampler, launch)
	if measureErr == nil || ctx.Err() != nil || command.ProcessState == nil {
		t.Fatalf("invalid preparation did not fail and reap: %+v %v %v", measured, measureErr, ctx.Err())
	}

	if measured.DescriptorCeiling != nil || measured.DescriptorSamples != 0 || measured.SampleAttempts != 0 {
		t.Fatalf("invalid preparation invented resource evidence: %+v", measured)
	}
}

func TestCanceledDescriptorPreparationKillsAndReapsOwnedProcess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	command := exectest.Command(ctx, controlledShellExecutable, "-c", "exec /bin/sleep 30")

	launch, err := prepareDescriptorLaunch(ctx, command, 64)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if releaseErr := launch.release(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})

	command.Path = controlledShellExecutable

	command.Args = []string{"sh", "-c", "exec /bin/sleep 30"}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}

	cancel()

	_, observeErr := launch.observe(ctx, command.Process.Pid)

	waitErr := command.Wait()
	if observeErr == nil || waitErr == nil || command.ProcessState == nil {
		t.Fatalf("canceled control did not fail and join real child: %v %v", observeErr, waitErr)
	}
}
