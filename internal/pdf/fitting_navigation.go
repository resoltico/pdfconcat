// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"cmp"
	"context"
	"maps"
	"math"
	"math/big"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

const binary64IntegerBoundary = 1 << 53

func destinationFloat(v types.Object) float64 {
	if n, ok := v.(types.Integer); ok {
		return float64(n)
	}
	n, _ := v.(types.Float)
	return float64(n)
}

func exactDestinationNumber(v types.Object) *big.Rat {
	if n, ok := v.(types.Integer); ok {
		return new(big.Rat).SetInt64(int64(n))
	}
	return new(big.Rat).SetFloat64(destinationFloat(v))
}

// Compare below the boundary without allocation. At its edges distinct
// integers can share a binary64 approximation, even across PDF number types.
func compareDestinationNumbers(a, b types.Object) int {
	x, y := destinationFloat(a), destinationFloat(b)
	if x != y || math.Abs(x) < binary64IntegerBoundary {
		return cmp.Compare(x, y)
	}
	return exactDestinationNumber(a).Cmp(exactDestinationNumber(b))
}

func mapDestinationNumber(v types.Object, scale, offset float64) (types.Object, bool) {
	if v == nil {
		return nil, true
	}
	n := destinationFloat(v)
	mapped := math.FMA(n, scale, offset)
	if math.Abs(n) >= binary64IntegerBoundary || math.Abs(mapped) >= binary64IntegerBoundary {
		// Apply the affine to the exact integer or parsed binary64 real.
		// Prefer an exact PDF integer even when it has no binary64 representation.
		exact := exactDestinationNumber(v)
		exact.Mul(exact, new(big.Rat).SetFloat64(scale))
		exact.Add(exact, new(big.Rat).SetFloat64(offset))
		if exact.IsInt() && exact.Num().IsInt64() {
			i := exact.Num().Int64()
			if int64(int(i)) == i {
				return types.Integer(i), true
			}
		}
		var representable bool
		mapped, representable = exact.Float64()
		if !representable {
			return nil, false
		}
	}
	return types.Float(mapped), !math.IsInf(mapped, 0) && !math.IsNaN(mapped)
}

func mapDestinationPoint(in types.Array, m matrix.Matrix) (types.Array, bool) {
	out := make(types.Array, 2)
	for axis := range out {
		input := axis
		if m[input][axis] == 0 {
			input = 1 - input
		}
		var ok bool
		out[axis], ok = mapDestinationNumber(in[input], m[input][axis], m[2][axis])
		if !ok {
			return nil, false
		}
	}
	return out, true
}

func (d explicitDestination) fit(target pageFitting) (types.Array, diag.Explanation) {
	m := target.Matrix
	bad := diag.FittingDestinationUnrepresentable()
	var out types.Array
	switch d.mode {
	case "XYZ":
		if len(d.operands) != 3 {
			return nil, bad
		}
		var ok bool
		out, ok = mapDestinationPoint(d.operands[:2], m)
		if !ok {
			return nil, bad
		}
		out = append(out, d.operands[2])
	case "FitR":
		if len(d.operands) != 4 || slices.Contains(d.operands, nil) {
			return nil, bad
		}
		for axis := range 2 {
			if compareDestinationNumbers(d.operands[axis], d.operands[axis+2]) >= 0 {
				return nil, bad
			}
		}
		// Fitting only scales and permutes or reverses axes. The two
		// opposite corners provide both extrema on each output axis.
		lower, lowerOK := mapDestinationPoint(d.operands[:2], m)
		upper, upperOK := mapDestinationPoint(d.operands[2:], m)
		if !lowerOK || !upperOK {
			return nil, bad
		}
		for axis := range 2 {
			switch compareDestinationNumbers(lower[axis], upper[axis]) {
			case 0:
				return nil, bad
			case 1:
				lower[axis], upper[axis] = upper[axis], lower[axis]
			}
		}
		out = types.Array{lower[0], lower[1], upper[0], upper[1]}
	case "FitH", "FitBH", "FitV", "FitBV":
		if len(d.operands) != 1 {
			return nil, bad
		}
		if d.operands[0] == nil {
			return nil, diag.Explanation{}
		}
		if target.Native.Rotate%360 != 0 {
			return nil, diag.FittingAxisDestinationRotated()
		}
		axis := 0
		if d.mode == "FitH" || d.mode == "FitBH" {
			axis = 1
		}
		value, ok := mapDestinationNumber(d.operands[0], m[axis][axis], m[2][axis])
		if !ok {
			return nil, bad
		}
		out = types.Array{value}
	default:
		return nil, diag.Explanation{}
	}
	changed := false
	for i, value := range out {
		original := d.operands[i]
		if value == original || value != nil && original != nil && compareDestinationNumbers(value, original) == 0 {
			out[i] = original
		} else {
			changed = true
		}
	}
	if !changed {
		return nil, diag.Explanation{}
	}
	return append(types.Array{d.mode}, out...), diag.Explanation{}
}

type fittedNavigation struct {
	findings []diag.Diagnostic
	slots    []navigationSlot
	named    map[string]destination
}

func planFittedNavigation(c context.Context, s *model.Context, pages []pageGeometry, plans fittingPlan, origins sourceOrigins) (fittedNavigation, error) {
	var result fittedNavigation
	if !anyTransform(plans) {
		return result, nil
	}
	targets := make(map[types.IndirectRef]pageFitting)
	for i, page := range pages {
		if plan := plans.page(i); plan.Decision.Transform {
			targets[page.ref] = plan
		}
	}
	mapTarget := func(d explicitDestination, sites []sourceSite) types.Array {
		target, ok := targets[d.page]
		if !ok {
			return nil
		}
		tail, problem := d.fit(target)
		if problem != (diag.Explanation{}) || tail != nil {
			for _, site := range sites {
				code, message := diag.FitDestinationMapped, diag.FittingDestinationMapped()
				if tail == nil {
					code, message = diag.FitDestinationUnsupported, problem
				}
				finding := diag.Report(code, message)
				finding.PDFLocation = site.location()
				diag.AttachEvidence(&finding, diag.FittingDestinationTarget(int64(target.Original), int64(d.page.ObjectNumber.Value()), int64(d.page.GenerationNumber.Value())))
				result.findings = append(result.findings, finding)
			}
		}
		return tail
	}
	named, err := readDestinations(c, s)
	if err != nil {
		return result, err
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		d := named[name]
		if _, ok := targets[d.target]; !ok {
			continue
		}
		sites := d.sites
		if original, ok := origins.destinations[name]; ok {
			sites = original.sites
		}
		explicit, e := readExplicitDestination(s, append(types.Array{d.target}, d.tail...))
		if e != nil {
			return result, e
		}
		if tail := mapTarget(explicit, sites); tail != nil {
			d.tail = tail
			named[name] = d
			result.named = named
		}
	}
	edits := map[navigationOwner]bool{}
	reported := map[sourceSite]bool{}
	err = walkRetained(c, s, origins, nil, func(binding retainedTarget) (bool, error) {
		site, edge := binding.site, binding.edge
		array, ok := binding.value.(types.Array)
		if !ok || len(array) == 0 {
			return true, nil
		}
		ref, _ := array[0].(types.IndirectRef)
		if _, transformed := targets[ref]; !transformed {
			return true, nil
		}
		d, e := readExplicitDestination(s, array)
		if e != nil {
			return false, site.wrap(e)
		}
		if edge.ref.ObjectNumber.Value() > 0 {
			site = edge
		}
		var sites []sourceSite
		if !reported[site] {
			sites = []sourceSite{site}
			reported[site] = true
		}
		if tail := mapTarget(d, sites); tail != nil {
			slot := ownerOf(binding.owner, binding.key)
			if !edits[slot] {
				result.slots = append(result.slots, navigationSlot{binding.owner, binding.key, append(types.Array{d.page}, tail...)})
				edits[slot] = true
			}
		}
		return true, nil
	}, nil, false)
	return result, err
}

func (p fittedNavigation) apply(c context.Context, s *model.Context) error {
	for _, slot := range p.slots {
		slot.owner[slot.key] = slot.value
	}
	if p.named != nil {
		return rebuildDestinations(c, s, p.named)
	}
	return c.Err()
}
