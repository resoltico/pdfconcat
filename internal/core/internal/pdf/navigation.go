// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"

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

func pruneNavigation(c context.Context, s *model.Context) (bool, error) {
	named, err := readDestinations(c, s)
	if err != nil {
		return false, err
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return false, err
	}
	selected := map[types.IndirectRef]bool{}
	for _, p := range pages {
		selected[p.ref] = true
	}
	forest, err := readOutlineForest(c, s)
	if err != nil {
		return false, err
	}
	removed := false
	err = walkRetained(c, s, func(types.Dict) error { return nil }, func(d types.Dict, key string) (bool, error) {
		keep, err := localTarget(s, d[key], named, selected)
		if !keep && err == nil {
			removed = true
		}
		return keep, err
	}, true)
	if err != nil {
		return false, err
	}
	for key, d := range named {
		if d.page < 1 {
			delete(named, key)
			removed = true
		}
	}
	cat, err := s.Catalog()
	if err != nil {
		return false, err
	}
	delete(cat, "Dests")
	names, err := s.DereferenceDict(cat["Names"])
	if err != nil {
		return false, err
	}
	delete(names, "Dests")
	delete(s.Names, "Dests")
	if err := rebuildDestinations(c, s, named); err != nil {
		return false, err
	}
	forest, pruned := pruneOutlineForest(forest)
	removed = removed || pruned
	if err := installOutlineForest(c, s, forest); err != nil {
		return false, err
	}
	return removed, nil
}
