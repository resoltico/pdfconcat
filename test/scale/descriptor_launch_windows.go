// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

//go:build windows

package scale

import (
	"context"
	"os/exec"
)

func prepareDescriptorLaunch(context.Context, *exec.Cmd, uint64) (*descriptorLaunch, error) {
	return &descriptorLaunch{}, nil
}

func (*descriptorLaunch) observe(context.Context, int) (DescriptorCeiling, error) {
	return DescriptorCeiling{}, nil
}

func (*descriptorLaunch) unchangedBinary() error { return nil }
