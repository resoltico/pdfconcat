// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build darwin

package scale

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/resoltico/pdfconcat/internal/exectest"
)

func TestVerifiedDescriptorCeilingCannotRepairActualFailedReading(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	command := exectest.Command(ctx, executable, "-test.run=^TestLimitedExecFixture$")

	command.Env = append(os.Environ(), limitFixtureMode+"=held")

	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	launch, err := prepareDescriptorLaunch(ctx, command, 64)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if releaseErr := launch.release(); releaseErr != nil {
			t.Error(releaseErr)
		}
	})

	sampler, err := prepareProcessSampler(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var (
		once       sync.Once
		releaseErr error
	)

	samples := 0
	spec := RunSpec{Binary: executable, OnSample: func() {
		samples++
		if samples == 1 {
			sampler.collector = failedCollectorExecutable
			return
		}

		once.Do(func() { _, releaseErr = fmt.Fprint(input, "stdio witness\nrelease\n") })
	}}

	measured, measureErr := measurePreparedCommand(ctx, spec, command, sampler, launch)
	if measureErr != nil || releaseErr != nil || ctx.Err() != nil || measured.ExitCode != 0 {
		t.Fatalf("actual capped child failed: %+v %v %v", measured, measureErr, releaseErr)
	}

	assertFailedQualityUnderCeiling(t, &measured)
}

func assertFailedQualityUnderCeiling(t *testing.T, measured *Measurement) {
	t.Helper()

	if !measured.DescriptorBoundVerified() || measured.DescriptorState != failedReading ||
		measured.DescriptorSamples < 1 || len(measured.ReadingFailures) == 0 || measured.MaxDescriptors == 64 {
		t.Fatalf("native ceiling laundered observation quality or replaced peak: %+v", measured)
	}
}
