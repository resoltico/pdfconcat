// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

//go:build !darwin && !linux

package main

import (
	"context"
	"fmt"
)

func (*command) runFuzzProcess(_ context.Context) error {
	return requireFuzzPlatform()
}

func requireFuzzPlatform() error {
	return fmt.Errorf("%w: owned fuzz process cleanup requires a macOS or Linux runner; no fuzz subprocess was started", errGate)
}
