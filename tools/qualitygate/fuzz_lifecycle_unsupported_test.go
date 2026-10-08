// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build !darwin && !linux

package main

import (
	"strings"
	"testing"
)

func TestFuzzLauncherRequiresOwnedProcessPlatformBeforeStarting(t *testing.T) {
	t.Parallel()

	run := &command{name: "not-installed-fuzz-fixture", fuzzLifecycle: true}
	if err := run.run(t.Context()); err == nil || !strings.Contains(err.Error(), "macOS or Linux runner") {
		t.Fatalf("unsupported platform reached child launch: %v", err)
	}
}

func TestFuzzGateRequiresOwnedPlatformBeforeDiscovery(t *testing.T) {
	t.Parallel()

	if err := runFuzz(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "macOS or Linux runner") {
		t.Fatalf("unsupported platform reached fuzz discovery: %v", err)
	}
}
