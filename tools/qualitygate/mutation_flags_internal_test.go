// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package main

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestMutationFlagsBoundActualWorkersAndReserveCleanup(t *testing.T) {
	t.Parallel()

	defaults, defaultErr := parseMutationFlags(nil)
	if defaultErr != nil || defaults.workers != 2 || defaults.maxDuration != 0 {
		t.Fatalf("default actual workers/duration: %+v %v", defaults, defaultErr)
	}

	for _, args := range [][]string{
		{"-workers=0"},
		{"-workers=-1"},
		{"-workers=5"},
		{"-timeout-coefficient=0"},
		{"-timeout-coefficient=-1"},
		{"-max-duration=-1s"},
		{"-max-duration=60s"},
		{"-max-duration=1s"},
	} {
		if _, parseErr := parseMutationFlags(args); parseErr == nil {
			t.Fatalf("invalid execution constraints accepted: %v", args)
		}
	}

	configured, configuredErr := parseMutationFlags([]string{"-workers=4", "-max-duration=2h"})
	if configuredErr != nil || configured.workers != maxMutationWorkers || configured.maxDuration != 2*time.Hour {
		t.Fatalf("explicit user campaign constraints: %+v %v", configured, configuredErr)
	}
}

func TestMutationArgumentsTranslateActualWorkersForPinnedPool(t *testing.T) {
	t.Parallel()

	for actual := 1; actual <= maxMutationWorkers; actual++ {
		for _, integration := range []bool{false, true} {
			options := mutationOptions{workers: actual, coefficient: defaultTimeoutCoefficient, integration: integration}
			args := gremlinsArguments("report.json", &options, nil)

			want := actual
			if integration && actual > 1 {
				want *= 2
			}

			if got := mutationArgumentValue(args, "--workers"); got != strconv.Itoa(want) {
				t.Fatalf("actual=%d integration=%t upstream=%q want=%d", actual, integration, got, want)
			}
		}
	}
}

func TestMutationDeadlineChargesSetupAndReservesCleanup(t *testing.T) {
	t.Parallel()

	started := time.Now().Add(-time.Minute)

	ctx, cancel := mutationContext(t.Context(), started, 2*time.Hour)
	defer cancel()

	deadline, present := ctx.Deadline()
	if !present || !deadline.Equal(started.Add(119*time.Minute)) {
		t.Fatalf("work deadline must reserve cleanup from command entry: %v/%t", deadline, present)
	}

	args, durationErr := mutationDurationArgument(ctx, 2*time.Hour)

	remaining, parseErr := time.ParseDuration(mutationArgumentValue(args, "--max-duration"))
	if durationErr != nil || parseErr != nil || remaining <= 0 || remaining > 118*time.Minute {
		t.Fatalf("setup time was not charged to tool budget: %v %v %v", args, durationErr, parseErr)
	}

	withoutBudget, stop := mutationContext(t.Context(), started, 0)
	defer stop()

	if optionalArgs, argumentErr := mutationDurationArgument(withoutBudget, 0); argumentErr != nil || len(optionalArgs) != 0 {
		t.Fatalf("optional deadline fabricated: %v %v", optionalArgs, argumentErr)
	}

	expired, finish := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer finish()

	if _, argumentErr := mutationDurationArgument(expired, 2*time.Hour); argumentErr == nil {
		t.Fatal("expired work budget allowed another stage")
	}
}

func mutationArgumentValue(args []string, flag string) string {
	for index, arg := range args {
		if arg == flag && index+1 < len(args) {
			return args[index+1]
		}
	}

	return ""
}
