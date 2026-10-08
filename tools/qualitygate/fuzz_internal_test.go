// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"context"
	"strings"
	"testing"
)

func TestFuzzBudgetRejectsInvalidValuesBeforeDiscovery(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "0s", "0x", "-1s", "-1x", "bad", "1.5x", "9223372036854775808x"} {
		if err := validateFuzzBudget(value); err == nil {
			t.Fatalf("invalid budget accepted: %q", value)
		}
	}

	for _, value := range []string{"1x", "+2x", "1ns", "1s", "1.5s", "1m30s"} {
		if err := validateFuzzBudget(value); err != nil {
			t.Fatalf("valid budget %q rejected: %v", value, err)
		}
	}
}

func TestCanceledFuzzTargetDoesNotStartGo(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := runFuzzTarget(ctx, t.TempDir(), fuzzTarget{pkg: "no-package", name: "FuzzMissing"}, "1x")
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("canceled child: %v", err)
	}
}
