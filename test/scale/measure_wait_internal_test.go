// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build unix

package scale

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type sampledInputFailure struct {
	canceled <-chan struct{}
	sampled  <-chan struct{}
}

var (
	errMeasuredInput       = errors.New("controlled stdin reader failure")
	errInputSampleCanceled = errors.New("controlled input canceled before sampling")
)

func (r sampledInputFailure) Read([]byte) (int, error) {
	select {
	case <-r.sampled:
		return 0, errMeasuredInput
	case <-r.canceled:
		return 0, errInputSampleCanceled
	}
}

func TestUnexpectedWaitErrorPreservesRealOutputExitAndResourceFacts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sampled := make(chan struct{})

	var once sync.Once

	data := "actual cat output before reader failure\n"
	spec := RunSpec{
		Binary:   "/bin/cat",
		Stdin:    io.MultiReader(strings.NewReader(data), sampledInputFailure{canceled: ctx.Done(), sampled: sampled}),
		OnSample: func() { once.Do(func() { close(sampled) }) },
	}

	measured, err := Measure(ctx, spec)
	if !errors.Is(err, errMeasuredInput) {
		t.Fatalf("real Wait I/O error lost: %+v %v", measured, err)
	}

	if measured.Stdout != data || measured.ExitCode != 0 || measured.Wall <= 0 || measured.PeakRSSBytes <= 0 ||
		measured.DescriptorSamples <= 0 ||
		measured.SampleAttempts < measured.DescriptorSamples {
		t.Fatalf("known facts lost on failed measurement: %+v", measured)
	}
}

func TestFailedProcessStartDoesNotInventSuccessfulExit(t *testing.T) {
	t.Parallel()

	measured, err := Measure(t.Context(), RunSpec{Binary: t.TempDir() + "/missing-executable"})
	if err == nil || measured.ExitCode != -1 || measured.RSSState != unavailableReading || measured.DescriptorState != unavailableReading ||
		measured.SampleAttempts != 0 {
		t.Fatalf("failed start invented execution facts: %+v %v", measured, err)
	}
}
