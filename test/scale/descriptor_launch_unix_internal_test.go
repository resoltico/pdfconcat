// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build unix

package scale

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

type limitedExecChild struct {
	command *exec.Cmd
	launch  *descriptorLaunch
	input   io.WriteCloser
	output  *bufio.Reader
	stderr  bytes.Buffer
	waited  bool
}

const (
	limitFixtureMode          = "PDFCONCAT_SCALE_LIMIT_EXEC_FIXTURE"
	preloadFixtureValue       = "LD_PRELOAD=unsafe"
	controlledShellExecutable = "/bin/sh"
	missingExecutableFixture  = "missing"
)

func TestLimitedExecFixture(t *testing.T) {
	t.Parallel()

	mode := os.Getenv(limitFixtureMode)
	if mode == "" {
		return
	}

	if mode != "held" {
		t.Fatal("unknown limited exec fixture")
	}

	limitExecFixture(t)
}

func limitExecFixture(t *testing.T) {
	t.Helper()

	verifyLimitFixtureDescriptors(t)

	input := bufio.NewReader(os.Stdin)

	line, err := input.ReadString('\n')
	if err != nil || line != "stdio witness\n" {
		t.Fatalf("inherited stdin: %q %v", line, err)
	}

	if _, err = fmt.Fprintln(os.Stderr, "stderr after exec"); err != nil {
		t.Fatal(err)
	}

	if _, err = fmt.Fprintln(os.Stdout, "alive after exec"); err != nil {
		t.Fatal(err)
	}

	line, err = input.ReadString('\n')
	if err != nil || line != "release\n" {
		t.Fatalf("owned release: %q %v", line, err)
	}
}

func startLimitedExecChild(ctx context.Context, t *testing.T) *limitedExecChild {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	child := &limitedExecChild{command: exectest.Command(ctx, executable, "-test.run=^TestLimitedExecFixture$", "-test.v")}

	child.command.Env = append(os.Environ(), limitFixtureMode+"=held")
	addInheritedLimitDescriptors(t, child.command)
	child.configureIO(t)

	child.launch, err = prepareDescriptorLaunch(ctx, child.command, 64)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if releaseErr := child.launch.release(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})

	if err = child.command.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { child.cleanup(t) })

	return child
}

func addInheritedLimitDescriptors(t *testing.T, command *exec.Cmd) {
	t.Helper()

	inherited, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := inherited.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	for range 80 {
		command.ExtraFiles = append(command.ExtraFiles, inherited)
	}
}

func (child *limitedExecChild) configureIO(t *testing.T) {
	t.Helper()

	var err error

	child.input, err = child.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	output, err := child.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	child.output = bufio.NewReader(output)
	child.command.Stderr = &child.stderr
}

func (child *limitedExecChild) cleanup(t *testing.T) {
	t.Helper()

	if child.waited {
		return
	}

	if killErr := child.command.Process.Kill(); killErr != nil {
		t.Error(killErr)
	}

	if waitErr := child.command.Wait(); waitErr == nil {
		t.Error("cleanup kill returned success")
	}
}

func (child *limitedExecChild) verifyLiveAfterControlEOF(ctx context.Context, t *testing.T) DescriptorCeiling {
	t.Helper()

	fact, err := child.launch.observe(ctx, child.command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}

	if fact.PID != child.command.Process.Pid {
		t.Fatal("launch identity changed")
	}

	if _, err = fmt.Fprintln(child.input, "stdio witness"); err != nil {
		t.Fatal(err)
	}

	var transcript bytes.Buffer

	for {
		line, readErr := child.output.ReadString('\n')
		transcript.WriteString(line)

		if readErr != nil {
			t.Fatalf("limited exec fixture failed before live acknowledgement: %v\n%s", readErr, transcript.String())
		}

		if line == "alive after exec\n" {
			return fact
		}
	}
}

func (child *limitedExecChild) finish(ctx context.Context, t *testing.T) {
	t.Helper()

	if _, err := fmt.Fprintln(child.input, "release"); err != nil {
		t.Fatal(err)
	}

	err := child.command.Wait()
	child.waited = true

	if err != nil || ctx.Err() != nil {
		t.Fatalf("actual exec failed: %v %v", err, ctx.Err())
	}

	if child.stderr.String() != "stderr after exec\n" {
		t.Fatalf("stderr inheritance: %q", child.stderr.String())
	}
}

func TestLimitedExecClosesInheritedDescriptorsAndControlObject(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	child := startLimitedExecChild(ctx, t)
	child.verifyLiveAfterControlEOF(ctx, t)
	child.finish(ctx, t)
}

func TestDescriptorLaunchRejectsEffectiveLoaderInterposition(t *testing.T) {
	t.Parallel()

	for _, environment := range [][]string{
		{preloadFixtureValue}, {"DYLD_INSERT_LIBRARIES=unsafe"}, {"LD_PRELOAD=", preloadFixtureValue},
	} {
		if err := checkLoaderEnvironment(environment); err == nil {
			t.Fatal("effective loader override accepted")
		}
	}

	if err := checkLoaderEnvironment([]string{preloadFixtureValue, "LD_PRELOAD=", "OTHER=ok"}); err != nil {
		t.Fatal("overwritten empty loader value is ineffective", err)
	}
}

func TestDescriptorLaunchRefusesUnsupportedCeilingAndMissingExecutable(t *testing.T) {
	t.Parallel()

	command := exectest.Command(t.Context(), filepath.Join(t.TempDir(), missingExecutableFixture))
	if _, err := prepareDescriptorLaunch(t.Context(), command, 65); err == nil {
		t.Fatal("unsupported ceiling accepted")
	}

	if _, err := prepareDescriptorLaunch(t.Context(), command, 64); err == nil || !strings.Contains(err.Error(), missingExecutableFixture) {
		t.Fatalf("missing executable not rejected: %v", err)
	}
}

func TestDescriptorPreparationRejectsContradictoryNativeFacts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	child := startLimitedExecChild(ctx, t)
	fact := child.verifyLiveAfterControlEOF(ctx, t)
	child.finish(ctx, t)

	if err := child.launch.validate(&fact, fact.PID); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		change func(*DescriptorCeiling)
		name   string
	}{
		{name: "pid", change: func(c *DescriptorCeiling) { c.PID++ }},
		{name: "uid", change: func(c *DescriptorCeiling) { c.UID++ }},
		{name: "token", change: func(c *DescriptorCeiling) { c.Token = "wrong" }},
		{name: "soft", change: func(c *DescriptorCeiling) { c.Soft++ }},
		{name: "hard", change: func(c *DescriptorCeiling) { c.Hard++ }},
		{name: "raise", change: func(c *DescriptorCeiling) { c.RaiseErrno = 0 }},
		{name: "device", change: func(c *DescriptorCeiling) { c.Device++ }},
		{name: "bytes", change: func(c *DescriptorCeiling) { c.Bytes++ }},
		{name: "hygiene", change: func(c *DescriptorCeiling) { c.HygieneConfigured = false }},
		{name: "control", change: func(c *DescriptorCeiling) { c.ControlCloseOnExec = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			altered := fact
			test.change(&altered)

			if err := child.launch.validate(&altered, fact.PID); err == nil {
				t.Fatal("contradictory native preparation accepted")
			}
		})
	}
}

func TestDescriptorResourcePredicateRejectsAlteredNativePreparation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	child := startLimitedExecChild(ctx, t)
	fact := child.verifyLiveAfterControlEOF(ctx, t)
	child.finish(ctx, t)

	measured := Measurement{ExitCode: 0, DescriptorCeiling: &fact}
	if !measured.DescriptorBoundVerified() {
		t.Fatal("actual native preparation not recognized")
	}

	baseline := measured

	for _, test := range []struct {
		change func(*DescriptorCeiling)
		name   string
	}{
		{name: "raise", change: func(c *DescriptorCeiling) { c.RaiseErrno = 0 }},
		{name: "unknown hygiene", change: func(c *DescriptorCeiling) { c.Hygiene = "unknown" }},
		{name: "invocation", change: func(c *DescriptorCeiling) { c.InvocationSHA256 = "" }},
		{name: "compiler path", change: func(c *DescriptorCeiling) { c.CompilerPath = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			altered := fact
			test.change(&altered)

			row := baseline

			row.DescriptorCeiling = &altered
			if row.DescriptorBoundVerified() {
				t.Fatal("altered native fact became verified resource evidence")
			}
		})
	}

	measured.MaxDescriptors = 65
	if measured.DescriptorBoundVerified() {
		t.Fatal("contradictory actual descriptor count accepted")
	}
}
