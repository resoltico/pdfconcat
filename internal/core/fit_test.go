// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"math"
	"testing"
)

func TestFitDecisionUsesFixedOrderedPhysicalTarget(t *testing.T) {
	target := Size{Width: 100, Height: 200}
	for _, tc := range []struct {
		size      Size
		transform bool
	}{
		{target, false}, {Size{100.01, 200.01}, false}, {Size{math.Nextafter(100.01, math.Inf(1)), 200}, true}, {Size{100.009, 200.009}, false}, {Size{100.011, 200}, true},
		{Size{200, 100}, true}, {Size{100.019, 200}, true},
	} {
		got, err := DecideFit(tc.size, &target)
		if err != nil || got.Transform != tc.transform {
			t.Fatalf("%+v: %+v %v", tc, got, err)
		}
		want := tc.size
		if tc.transform {
			want = target
		}
		if got.Output != want {
			t.Fatal("expected geometry was snapped or lost")
		}
	}
	a4, _ := ParseSize("A4", false)
	for _, tc := range []struct {
		size      Size
		transform bool
	}{{Size{595.28, 841.89}, false}, {Size{595, 842}, true}} {
		got, err := DecideFit(tc.size, &a4)
		if err != nil || got.Transform != tc.transform {
			t.Fatal(tc, got, err)
		}
	}
	for _, size := range []Size{{}, {-1, 2}, {math.NaN(), 2}, {2, math.Inf(1)}} {
		if _, err := DecideFit(size, nil); err == nil {
			t.Fatal("invalid source accepted", size)
		}
	}
}
func TestPageSizeRunsRequireExactAdjacentBothSizes(t *testing.T) {
	var runs []PageSizeRun
	for _, p := range []struct {
		page int
		size Size
	}{{2, Size{100, 200}}, {3, Size{100, 200}}, {5, Size{100, 200}}, {6, Size{100.001, 200}}} {
		fit, err := DecideFit(p.size, nil)
		if err != nil {
			t.Fatal(err)
		}
		runs = AppendPageSize(runs, p.page, fit)
	}
	if len(runs) != 3 || runs[0].Source != [2]int{2, 3} || runs[1].Source != [2]int{5, 5} || runs[2].Source != [2]int{6, 6} {
		t.Fatal(runs)
	}
}

func TestPageRunsNeverMergeDifferentOutputExpectations(t *testing.T) {
	source := Size{100, 200}
	runs := AppendPageSize(nil, 1, FitDecision{Source: source, Output: source})
	runs = AppendPageSize(runs, 2, FitDecision{Source: source, Output: Size{300, 400}, Transform: true})
	if len(runs) != 2 {
		t.Fatal("different expected output sizes merged", runs)
	}
}
