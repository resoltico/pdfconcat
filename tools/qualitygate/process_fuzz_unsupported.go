// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build !darwin && !linux

package main

import (
	"context"
	"fmt"
)

func (*command) runFuzzProcess(_ context.Context) error {
	return fuzzPlatformError()
}

func runPlatformFuzz(context.Context, []string, string) error {
	return fuzzPlatformError()
}

func fuzzPlatformError() error {
	return fmt.Errorf("%w: owned fuzz process cleanup requires a macOS or Linux runner; no fuzz subprocess was started", errGate)
}
