// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package plan

import "testing"

func TestExactDecimalMagnitudeNearIntegerBounds(t *testing.T) {
	for _, row := range []struct {
		text  string
		bound int64
		want  int
	}{
		{"10", 10, 0},
		{"12", 12, 0},
		{"12", 19, -1},
		{"10.0000000000000001", 10, 1},
		{"9.9999999999999999", 10, -1},
		{"0.999999999999999999999999", 1, -1},
		{"1.000000000000000001", 1, 1},
		{"100e-1", 10, 0},
		{"10000000000000001e-15", 10, 1},
		{"99999999999999999e-16", 10, -1},
		{"1e99999999999999999999", 10, 1},
		{"1e-99999999999999999999", 10, -1},
		{"0e99999999999999999999", 1, -1},
		{"-14400.0000000000001", 14400, 1},
	} {
		if got := compareDecimalMagnitude(row.text, row.bound); got != row.want {
			t.Errorf("%s vs %d: %d want %d", row.text, row.bound, got, row.want)
		}
	}
}
