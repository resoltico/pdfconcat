// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// FitTolerance is the absolute displayed-size match tolerance in physical points.
const FitTolerance = 0.01

// FitDecision owns both disposition and the expected displayed output geometry.
type FitDecision struct {
	Source, Output Size
	Transform      bool
}

// DecideFit compares ordered dimensions directly with the fixed target. The
// inclusive interval endpoints are rounded once by float64 arithmetic, rather
// than subtracting nearly equal values at the tolerance boundary.
func DecideFit(source Size, target *Size) (FitDecision, error) {
	positive := func(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !positive(source.Width) || !positive(source.Height) {
		return FitDecision{}, errors.New("source physical dimensions must be finite and positive")
	}
	d := FitDecision{Source: source, Output: source}
	if target == nil {
		return d, nil
	}
	if !validSize(*target, false) {
		return FitDecision{}, errors.New("fitting target is invalid")
	}
	match := func(a, b float64) bool { return a >= b-FitTolerance && a <= b+FitTolerance }
	if !match(source.Width, target.Width) || !match(source.Height, target.Height) {
		d.Transform = true
		d.Output = *target
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
	return fmt.Sprintf("Fit pages outside %g pt of this displayed-size target; matching pages keep their native geometry.", FitTolerance)
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
