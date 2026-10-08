// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package report

import "testing"

func TestStructuralCancellationUsesPositiveContiguousLowBitMask(t *testing.T) {
	t.Parallel()

	if cancelInterval <= 0 || cancelInterval&(cancelInterval+1) != 0 {
		t.Fatal("structural cancellation requires a positive contiguous low-bit mask for uniform cadence")
	}
}
