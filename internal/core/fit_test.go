// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"math"
	"math/big"
	"strings"
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
		got, err := DecideFit(tc.size, DefaultFitPolicy(target))
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
		got, err := DecideFit(tc.size, DefaultFitPolicy(a4))
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

func TestFitPolicyToleranceIntervals(t *testing.T) {
	target := Size{600, 800}
	if policy := DefaultFitPolicy(target); *policy != (FitPolicy{Target: target, Tolerance: .01}) {
		t.Fatal(policy)
	}
	nextUp := func(n float64) float64 { return math.Nextafter(n, math.Inf(1)) }
	nextDown := func(n float64) float64 { return math.Nextafter(n, math.Inf(-1)) }
	for _, tc := range []struct {
		tolerance float64
		source    Size
		move      bool
	}{
		{1, Size{599, 799}, false}, {1, Size{601, 801}, false},
		{1, Size{nextDown(599), 800}, true}, {1, Size{nextUp(601), 800}, true},
		{1, Size{600, nextDown(799)}, true}, {1, Size{600, nextUp(801)}, true},
		{0, target, false}, {0, Size{nextDown(600), 800}, true},
		{0, Size{nextUp(600), 800}, true}, {0, Size{600, nextDown(800)}, true},
		{0, Size{600, nextUp(800)}, true},
		{1, Size{800, 600}, true}, {200, Size{800, 600}, false},
		{1, Size{601.5, 801}, true},
		{14400, Size{.5, .75}, false}, {14400, Size{15000, 15200}, false},
		{14400, Size{nextUp(15000), 15200}, true},
	} {
		want := tc.source
		if tc.move {
			want = target
		}
		got, err := DecideFit(tc.source, &FitPolicy{Target: target, Tolerance: tc.tolerance})
		if err != nil || got != (FitDecision{Source: tc.source, Output: want, Transform: tc.move}) {
			t.Fatalf("%+v: %+v, %v", tc, got, err)
		}
	}
	for _, source := range []Size{{.5, .75}, {20000, 20000}} {
		got, err := DecideFit(source, nil)
		if err != nil || got != (FitDecision{Source: source, Output: source}) {
			t.Fatalf("native source: %+v, %v", got, err)
		}
	}
}

func TestFitPolicyAdmissionOwnsCanonicalCopy(t *testing.T) {
	fit := FitPolicy{Target: Size{600, 800}, Tolerance: math.Copysign(0, -1)}
	draft := Declaration{Fitting: &fit, Items: []Item{&PageItem{Count: 1}}}
	plan, ds := AdmitDeclaration(draft)
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := FitPolicy{Target: fit.Target, Tolerance: 0}
	first := plan.Fitting()
	if *first != want || math.Signbit(first.Tolerance) {
		t.Fatalf("policy was not canonical: %+v", first)
	}
	fit.Tolerance = 1
	first.Target = Size{300, 400}
	plan.Declaration().Fitting.Tolerance = 2
	layout, ds := Resolve(plan, nil, 0)
	if len(ds) != 0 || layout.Fitting == nil {
		t.Fatal(ds)
	}
	layout.Fitting.Tolerance = 3
	if *plan.Fitting() != want {
		t.Fatalf("mutation changed the admitted policy: %+v", plan.Fitting())
	}
	for _, value := range []float64{-1, math.NaN(), math.Inf(-1), math.Inf(1), math.MaxFloat64, math.Nextafter(14400, math.Inf(1))} {
		fit.Tolerance = value
		if _, ds := AdmitDeclaration(draft); len(ds) != 1 {
			t.Fatalf("admitted invalid tolerance %g: %+v", value, ds)
		}
	}
	for _, size := range []Size{{}, {.5, 500}, {14401, 600}, {math.NaN(), 700}} {
		draft.Fitting = DefaultFitPolicy(size)
		if _, ds := AdmitDeclaration(draft); len(ds) != 1 {
			t.Fatalf("admitted invalid target %+v: %+v", size, ds)
		}
	}
}

func TestAuthoredFitToleranceRoundsPhysicalUnitsOnce(t *testing.T) {
	for _, tc := range []struct {
		length string
		points float64
	}{
		{"1in", 72}, {"25.4mm", 72}, {"2.54cm", 72}, {"72pt", 72},
		{"0.01in", .72}, {"0.254mm", .72}, {"0.72pt", .72},
		{"-0pt", 0}, {"-0.00mm", 0},
	} {
		got, err := ParseFitToleranceLength(tc.length)
		if err != nil || got != tc.points || math.Signbit(got) {
			t.Fatalf("%s = %.17g, %v; want %g", tc.length, got, err, tc.points)
		}
	}
	for _, width := range []float64{73, math.Nextafter(73, math.Inf(1))} {
		tolerance, err := ParseFitToleranceLength("25.4mm")
		if err != nil {
			t.Fatal(err)
		}
		d, err := DecideFit(Size{Width: width, Height: 100}, &FitPolicy{Target: Size{Width: 1, Height: 100}, Tolerance: tolerance})
		if err != nil || d.Transform != (width > 73) {
			t.Fatalf("width %.17g: %+v %v", width, d, err)
		}
	}
}

func TestLongFitToleranceKeepsBinary64TieDirection(t *testing.T) {
	// Divide the exact midpoint at 72 by the inch ratio. Its decimal expansion
	// repeats, so inputs on opposite sides can agree through the cutoff.
	midpoint := new(big.Rat).SetFloat64(math.Nextafter(72, math.Inf(1)))
	midpoint.Add(midpoint, big.NewRat(72, 1))
	midpoint.Quo(midpoint, big.NewRat(144, 1))
	decimal := midpoint.FloatString(1200)
	below := decimal[:len(decimal)-1] + "0in"
	above := decimal[:len(decimal)-1] + "9in"
	for _, tc := range []struct {
		text string
		want float64
	}{{below, 72}, {above, math.Nextafter(72, math.Inf(1))}} {
		got, err := ParseFitToleranceLength(tc.text)
		if err != nil || got != tc.want {
			t.Fatalf("late midpoint distinction: got %.17g, %v; want %.17g", got, err, tc.want)
		}
	}
	if _, err := ParseFitToleranceLength("-0." + strings.Repeat("0", 4000) + "1mm"); err == nil {
		t.Fatal("accepted a negative nonzero length")
	}
}
