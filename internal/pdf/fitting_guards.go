// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"reflect"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type coordinateDestination struct {
	Target int
	Kind   types.Name
	Site   sourceSite
}

func fittingDestinations(c context.Context, s *model.Context, origins sourceOrigins) ([]coordinateDestination, error) {
	named, err := readDestinations(c, s)
	if err != nil {
		return nil, err
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, err
	}
	indices := map[types.IndirectRef]int{}
	for i, p := range pages {
		indices[p.ref] = origins.pages[p.ref]
		if indices[p.ref] == 0 {
			indices[p.ref] = i + 1
		}
	}
	var facts []coordinateDestination
	add := func(page int, tail types.Array, sites []sourceSite) {
		if page < 1 || !numericPosition(tail) {
			return
		}
		kind, _ := tail[0].(types.Name)
		if len(sites) == 0 {
			sites = []sourceSite{{}}
		}
		for _, site := range sites {
			facts = append(facts, coordinateDestination{Target: page, Kind: kind, Site: site})
		}
	}
	names := make([]string, 0, len(named))
	for name := range named {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		d := named[name]
		sites := d.sites
		if original, ok := origins.destinations[name]; ok {
			sites = original.sites
		}
		original := 0
		if d.page > 0 && d.page <= len(pages) {
			original = indices[pages[d.page-1].ref]
		}
		add(original, d.tail, sites)
	}
	err = walkRetained(c, s, origins, nil, func(d types.Dict, key string, site, _ sourceSite) (bool, error) {
		value, e := s.Dereference(d[key])
		if e != nil {
			return false, e
		}
		if name, e := destinationName(s, value); e == nil {
			target, ok := named[name]
			if !ok || target.page < 1 {
				return false, nil
			}
			original := 0
			if target.page <= len(pages) {
				original = indices[pages[target.page-1].ref]
			}
			add(original, target.tail, []sourceSite{site})
			return true, nil
		}
		array, ok := value.(types.Array)
		if !ok || len(array) < 2 {
			return true, nil
		}
		ref, ok := array[0].(types.IndirectRef)
		if !ok {
			return true, nil
		}
		tail := make(types.Array, len(array)-1)
		for i, v := range array[1:] {
			tail[i], e = s.Dereference(v)
			if e != nil {
				return false, e
			}
		}
		add(indices[ref], tail, []sourceSite{site})
		return true, nil
	}, nil, false)
	return facts, err
}

type annotationBindings struct {
	dict  types.Dict
	pages []int
	site  sourceSite
}

// Inventory actual mutable dictionaries, not equal values or shared AP streams.
func fittingOwnership(c context.Context, s *model.Context, pages []pageGeometry, plans fittingPlan, origins sourceOrigins) ([]diag.Diagnostic, error) {
	if !anyTransform(plans) {
		return nil, nil
	}
	bindings := map[any]*annotationBindings{}
	var ordered []*annotationBindings
	identity := func(d types.Dict) any { return reflect.ValueOf(d).UnsafePointer() }
	record := func(d types.Dict, site sourceSite) *annotationBindings {
		key := identity(d)
		b := bindings[key]
		if b == nil {
			b = &annotationBindings{dict: d, site: site}
			bindings[key] = b
			ordered = append(ordered, b)
		}
		return b
	}
	refs := map[types.IndirectRef]int{}
	for i, p := range pages {
		refs[p.ref] = i + 1
		array, err := s.DereferenceArray(p.dict["Annots"])
		if err != nil {
			return nil, err
		}
		for index, obj := range array {
			d, err := s.DereferenceDict(obj)
			if err != nil {
				return nil, err
			}
			if d == nil {
				continue
			}
			site := sourceSite{ref: p.ref, page: plans.page(i).Original}.key("Annots").index(index).anchor(obj, origins)
			b := record(d, site)
			b.pages = append(b.pages, i+1)
		}
	}
	err := walkRetained(c, s, origins, func(d types.Dict, site sourceSite) error {
		subtype, e := nameValue(s, d["Subtype"])
		if e != nil {
			return e
		}
		if subtype == "Widget" {
			record(d, site)
		}
		return nil
	}, func(types.Dict, string, sourceSite, sourceSite) (bool, error) { return true, nil }, nil, false)
	if err != nil {
		return nil, err
	}
	var ds []diag.Diagnostic
	for _, b := range ordered {
		changed := false
		owners := map[int]bool{}
		for _, page := range b.pages {
			owners[page] = true
			changed = changed || plans.page(page-1).Decision.Transform
		}
		add := func(code diag.Code, text diag.Explanation) {
			d := diag.Report(code, text)
			d.PDFLocation = b.site.location()
			ds = append(ds, d)
		}
		if len(b.pages) > 1 && changed {
			add(diag.FitAnnotationOwnershipUnsupported, diag.FittingAnnotationOwnershipUnsupported())
			continue
		}
		subtype, e := nameValue(s, b.dict["Subtype"])
		if e != nil {
			return nil, e
		}
		if subtype != "Widget" {
			continue
		}
		p, e := s.Dereference(b.dict["P"])
		if e != nil {
			return nil, e
		}
		// P itself is a page reference: retain its native identity before dereferencing.
		owner := 0
		if ref, ok := b.dict["P"].(types.IndirectRef); ok {
			owner = refs[ref]
		}
		conflict := len(owners) > 1 || p != nil && owner == 0
		if owner > 0 {
			for page := range owners {
				conflict = conflict || page != owner
			}
			owners[owner] = true
		}
		if len(owners) != 1 {
			conflict = true
		}
		for page := range owners {
			changed = changed || plans.page(page-1).Decision.Transform
		}
		if changed || conflict {
			add(diag.FitFormUnsupported, diag.FittingWidgetOwnershipUnsupported())
		}
	}
	return ds, nil
}

func fittingFindings(c context.Context, s *model.Context, pages []pageGeometry, plans fittingPlan, origins sourceOrigins) ([]diag.Diagnostic, error) {
	if !anyTransform(plans) {
		return nil, nil
	}
	composition, err := fittingCompositionFindings(c, s, pages, plans)
	if err != nil {
		return nil, err
	}
	ds, err := fittingOwnership(c, s, pages, plans, origins)
	if err != nil {
		return nil, err
	}
	ds = append(ds, composition...)
	destinations, err := fittingDestinations(c, s, origins)
	if err != nil {
		return nil, err
	}
	transform := map[int]bool{}
	for _, p := range plans.Pages {
		transform[p.Original] = p.Decision.Transform
	}
	for _, target := range destinations {
		if transform[target.Target] {
			d := diag.Report(diag.FitDestinationUnsupported, diag.FittingInternalPageDestinationsWithNumericPositionCoordinatesIsUnsupported())
			d.PDFLocation = target.Site.location()
			ds = append(ds, d)
		}
	}
	issue, err := annotationFittingIssue(c, s, pages, plans)
	if err != nil {
		return nil, err
	}
	if issue.Present() {
		d := diag.Report(diag.FitAnnotationUnsupported, diag.FittingAnnotationRefusal(issue))
		d.PDFLocation = &issue.Site
		ds = append(ds, d)
	}
	warnings, err := fittingPrintWarnings(s, pages, plans)
	if err != nil {
		return nil, err
	}
	ds = append(ds, warnings...)
	return ds, nil
}
