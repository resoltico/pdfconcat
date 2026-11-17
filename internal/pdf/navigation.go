// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func localTarget(s *model.Context, o types.Object, named map[string]destination, pages map[types.IndirectRef]bool) (bool, error) {
	value, err := s.Dereference(o)
	if err != nil {
		return false, err
	}
	if value == nil {
		return true, nil
	}
	if array, ok := value.(types.Array); ok {
		if len(array) == 0 {
			return false, nil
		}
		ref, ok := array[0].(types.IndirectRef)
		return ok && pages[ref], nil
	}
	name, err := destinationName(s, value)
	if err != nil {
		return false, err
	}
	target, ok := named[name]
	return ok && target.page > 0, nil
}

func pruneNavigation(c context.Context, s *model.Context, origins sourceOrigins) ([]navigationChange, error) {
	named, err := readDestinations(c, s)
	if err != nil {
		return nil, err
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, err
	}
	selected := map[types.IndirectRef]bool{}
	for _, p := range pages {
		selected[p.ref] = true
	}
	forest, err := readOutlineForest(c, s)
	if err != nil {
		return nil, err
	}
	changes := []navigationChange{}
	for name, d := range named {
		if original, ok := origins.destinations[name]; ok {
			d.sites = original.sites
			named[name] = d
		}
	}
	err = walkRetained(c, s, origins, nil, func(d types.Dict, key string, site, edge sourceSite) (bool, error) {
		keep, e := localTarget(s, d[key], named, selected)
		if e != nil {
			return false, site.wrap(e)
		}
		if !keep && e == nil {
			targetPage := 0
			value, e := s.Dereference(d[key])
			if e != nil {
				return false, e
			}
			if array, ok := value.(types.Array); ok && len(array) > 0 {
				if ref, ok := array[0].(types.IndirectRef); ok {
					targetPage = origins.pages[ref]
				}
			} else if name, e := destinationName(s, value); e == nil {
				targetPage = origins.destinations[name].page
			}
			change := navigationChange{site: site, targetPage: targetPage}
			if edge.ref.ObjectNumber.Value() > 0 {
				change.site = edge
				change.target = site
				change.action = true
			}
			changes = append(changes, change)
		}
		return keep, e
	}, nil, true)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(named))
	for key := range named {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		d := named[key]
		if d.page < 1 {
			delete(named, key)
			original := origins.destinations[key]
			if original.page > 0 {
				for _, site := range d.sites {
					changes = append(changes, navigationChange{site: site, targetPage: original.page, name: key})
				}
			}
		}
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	delete(cat, "Dests")
	names, err := s.DereferenceDict(cat["Names"])
	if err != nil {
		return nil, err
	}
	delete(names, "Dests")
	delete(s.Names, "Dests")
	if err := rebuildDestinations(c, s, named); err != nil {
		return nil, err
	}
	forest, _ = pruneOutlineForest(forest)
	if err := installOutlineForest(c, s, forest); err != nil {
		return nil, err
	}
	return changes, nil
}
