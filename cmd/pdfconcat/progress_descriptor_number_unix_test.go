//go:build darwin || linux

// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package main

import (
	"math"
	"os"
	"testing"
)

func TestProgressDescriptorNumberClassification(t *testing.T) {
	t.Parallel()
	// These are numeric namespace/ABI cases, not claims that the kernel allocated MaxInt32.
	for _, entry := range []struct {
		descriptor int
		want       error
	}{
		{-1, os.ErrInvalid},
		{0, os.ErrInvalid},
		{1, os.ErrInvalid},
		{2, os.ErrInvalid},
		{3, nil},
		{4, nil},
		{math.MaxInt32 - 1, nil},
		{math.MaxInt32, nil},
		{math.MaxInt32 + 1, errProgressDescriptorRange},
	} {
		number, got := checkedProgressDescriptorNumber(entry.descriptor)
		if got != entry.want {
			t.Errorf("descriptor number %d: %v, want %v", entry.descriptor, got, entry.want)
		}
		if got == nil && int(number) != entry.descriptor {
			t.Errorf("checked descriptor %d narrowed to %d", entry.descriptor, number)
		}
	}
}
