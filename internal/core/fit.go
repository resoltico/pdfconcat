// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// DefaultFitTolerance is the default absolute match tolerance in points.
const DefaultFitTolerance = 0.01

// FitPolicy compares displayed sides with a target using an absolute point tolerance.
type FitPolicy struct {
	Target    Size
	Tolerance float64
}

// DefaultFitPolicy supplies the tolerance for callers that omit it.
func DefaultFitPolicy(target Size) *FitPolicy {
	return &FitPolicy{Target: target, Tolerance: DefaultFitTolerance}
}

// NormalizeFitTolerance validates the physical length and canonicalizes signed zero.
func NormalizeFitTolerance(points float64) (float64, error) {
	if !finite(points) || points < 0 || points > MaxSide {
		return 0, fitToleranceRangeError()
	}
	return math.Abs(points), nil
}

// Normalize validates a complete policy, preserving an explicitly exact tolerance.
func (p FitPolicy) Normalize() (FitPolicy, error) {
	if !validSize(p.Target, false) {
		return FitPolicy{}, fmt.Errorf("fitting requires finite page sides from 1 through %g points", MaxSide)
	}
	tolerance, err := NormalizeFitTolerance(p.Tolerance)
	p.Tolerance = tolerance
	return p, err
}

// FitDecision owns both disposition and the expected displayed output geometry.
type FitDecision struct {
	Source, Output Size
	Transform      bool
}

// DecideFit compares ordered dimensions directly with the fixed target. The
// inclusive interval endpoints are rounded once by float64 arithmetic, rather
// than subtracting nearly equal values at the tolerance boundary.
func DecideFit(source Size, policy *FitPolicy) (FitDecision, error) {
	positive := func(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !positive(source.Width) || !positive(source.Height) {
		return FitDecision{}, errors.New("source physical dimensions must be finite and positive")
	}
	d := FitDecision{Source: source, Output: source}
	if policy == nil {
		return d, nil
	}
	fit, err := policy.Normalize()
	if err != nil {
		return FitDecision{}, err
	}
	match := func(a, b float64) bool { return a >= b-fit.Tolerance && a <= b+fit.Tolerance }
	if !match(source.Width, fit.Target.Width) || !match(source.Height, fit.Target.Height) {
		d.Transform = true
		d.Output = fit.Target
	}
	return d, nil
}

// PageSizeRun is one complete adjacent original-source run. OutputSize is a
// resolved expectation, independent of publication. Runs never bridge gaps.
type PageSizeRun struct {
	Source                 [2]int
	SourceSize, OutputSize Size
}

// AppendPageSize compacts only exact equality of both observed/expected sizes.
func AppendPageSize(runs []PageSizeRun, page int, fit FitDecision) []PageSizeRun {
	if n := len(runs); n > 0 {
		last := &runs[n-1]
		if last.Source[1]+1 == page && last.SourceSize == fit.Source && last.OutputSize == fit.Output {
			last.Source[1] = page
			return runs
		}
	}
	return append(runs, PageSizeRun{Source: [2]int{page, page}, SourceSize: fit.Source, OutputSize: fit.Output})
}

// ExpectedPageSize reads an owner-bound original-page record; missing geometry
// is a processing inconsistency, not a nominal fitting-size fallback.
func ExpectedPageSize(runs []PageSizeRun, original int) (Size, bool) {
	index := sort.Search(len(runs), func(i int) bool { return runs[i].Source[1] >= original })
	if index < len(runs) && original >= runs[index].Source[0] {
		return runs[index].OutputSize, true
	}
	return Size{}, false
}

// FittingDescription is the shared capability description used in help.
func FittingDescription() string {
	return "Fit pages outside the absolute tolerance on either ordered displayed side of the target. Matching pages keep their native geometry."
}

// FitToleranceDescription describes the resolved absolute match allowance.
func FitToleranceDescription() string {
	return fmt.Sprintf("Absolute tolerance in points from 0 through %g. Defaults to %g pt when omitted. Zero requires exact dimensions.", MaxSide, DefaultFitTolerance)
}

// FittingNavigationDescription describes fitting-specific ownership limits.
func FittingNavigationDescription() string {
	return "Fitting refuses numeric local destinations only on transformed targets, and widgets on transformed or unproved owners."
}

// FittingAppearanceDescription describes transformed-page appearance requirements.
func FittingAppearanceDescription() string {
	return "Transformed FreeText, Text icons, Caret, captioned Lines, rotated NoRotate and boundary-crossing annotations need normal appearances."
}

// FittingHitAreaDescription describes the transformed-page interaction boundary.
func FittingHitAreaDescription() string {
	return "On transformed pages, link hit areas and other annotation activation/hover/focus regions must stay inside the source frame."
}
