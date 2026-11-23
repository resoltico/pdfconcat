// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type navigationSlot struct {
	owner types.Dict
	key   string
	value types.Array
}

// Resolve a navigation object by its full identity. The unresolved value
// remains distinct from a live null so each slot can enforce its own policy.
func resolveNavigation(s *model.Context, o types.Object) (types.Object, bool, error) {
	if ref, indirect := o.(types.IndirectRef); indirect && !liveReference(s, ref) {
		return nil, false, nil
	}
	value, err := s.Dereference(o)
	return value, true, err
}

func navigationValue(s *model.Context, o types.Object, site sourceSite) (types.Object, error) {
	value, defined, err := resolveNavigation(s, o)
	if err == nil && !defined {
		if ref, ok := o.(types.IndirectRef); ok {
			err = refuse(diag.SourceUnsupported, diag.UndefinedNavigationReference(int64(ref.ObjectNumber.Value()), int64(ref.GenerationNumber.Value())))
		}
	}
	return value, site.wrap(err)
}

func navigationContainer[T types.Dict | types.Array](s *model.Context, o types.Object, site sourceSite) (T, error) {
	value, err := navigationValue(s, o, site)
	if err != nil || value == nil {
		return nil, err
	}
	container, ok := value.(T)
	if !ok {
		return nil, site.wrap(refuse(diag.SourceUnsupported, diag.InvalidNavigationBinding()))
	}
	return container, nil
}

func localTarget(value types.Object, named map[string]destination, pages map[types.IndirectRef]bool) (bool, error) {
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
	name, err := destinationName(value)
	if err != nil {
		return false, err
	}
	target, ok := named[name]
	return ok && target.page > 0, nil
}

type navigationProjection struct {
	removed  []navigationChange
	external []diag.Diagnostic
}

func pruneNavigation(c context.Context, s *model.Context, origins sourceOrigins, assembly bool) (navigationProjection, error) {
	var result navigationProjection
	actions, err := newExternalActionObserver(s, origins, assembly)
	if err != nil {
		return result, err
	}
	named, err := readDestinations(c, s)
	if err != nil {
		return result, err
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return result, err
	}
	selected := map[types.IndirectRef]bool{}
	for _, p := range pages {
		selected[p.ref] = true
	}
	links, err := linksBeforeProjection(c, s, pages, origins)
	if err != nil {
		return result, err
	}
	forest, err := readOutlineForest(c, s)
	if err != nil {
		return result, err
	}
	for name, d := range named {
		if original, ok := origins.destinations[name]; ok {
			d.sites = original.sites
			named[name] = d
		}
	}
	err = walkRetained(c, s, origins, nil, func(binding retainedTarget) (bool, error) {
		keep, e := localTarget(binding.value, named, selected)
		if e != nil {
			return false, binding.site.wrap(e)
		}
		if !keep {
			targetPage := 0
			if array, ok := binding.value.(types.Array); ok && len(array) > 0 {
				if ref, ok := array[0].(types.IndirectRef); ok {
					targetPage = origins.pages[ref]
				}
			} else if name, e := destinationName(binding.value); e == nil {
				targetPage = origins.destinations[name].page
			}
			change := navigationChange{site: binding.site, targetPage: targetPage}
			if binding.edge.ref.ObjectNumber.Value() > 0 {
				change.site = binding.edge
				change.target = binding.site
				change.action = true
			}
			result.removed = append(result.removed, change)
		}
		return keep, e
	}, actions.visit, true)
	if err != nil {
		return result, err
	}
	if err := pruneProjectedLinks(c, s, links); err != nil {
		return result, err
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
					result.removed = append(result.removed, navigationChange{site: site, targetPage: original.page, name: key})
				}
			}
		}
	}
	cat, err := s.Catalog()
	if err != nil {
		return result, err
	}
	delete(cat, "Dests")
	names, err := navigationContainer[types.Dict](s, cat["Names"], catalogSite(s).key("Names"))
	if err != nil {
		return result, err
	}
	if _, present := names["Dests"]; present {
		names = maps.Clone(names)
		delete(names, "Dests")
		cat["Names"] = names
	}
	delete(s.Names, "Dests")
	if err := rebuildDestinations(c, s, named); err != nil {
		return result, err
	}
	forest, _ = pruneOutlineForest(forest)
	if err := installOutlineForest(c, s, forest); err != nil {
		return result, err
	}
	result.external = actions.findings
	return result, nil
}
