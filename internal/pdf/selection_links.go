// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type projectedLinkPage struct {
	page  types.Dict
	array types.Array
	links []projectedLink
}

type projectedLink struct {
	dict  types.Dict
	site  sourceSite
	index int
}

func linksBeforeProjection(c context.Context, s *model.Context, pages []pageRef, origins sourceOrigins) ([]projectedLinkPage, error) {
	var result []projectedLinkPage
	for _, page := range pages {
		if err := c.Err(); err != nil {
			return nil, err
		}
		site := sourceSite{ref: page.ref, page: origins.pages[page.ref]}.key("Annots")
		array, err := navigationContainer[types.Array](s, page.dict["Annots"], site)
		if err != nil {
			return nil, err
		}
		entry := projectedLinkPage{page: page.dict, array: array}
		for i, raw := range array {
			where := site.index(i).anchor(raw, origins)
			link, err := navigationContainer[types.Dict](s, raw, where)
			if err != nil {
				return nil, err
			}
			kind, err := navigationValue(s, link["Subtype"], where.key("Subtype"))
			if err != nil {
				return nil, err
			}
			if kind != types.Name("Link") {
				continue
			}
			kept, err := linkHasBinding(s, link, where)
			if err != nil {
				return nil, err
			}
			if kept {
				entry.links = append(entry.links, projectedLink{link, where, i})
			}
		}
		if len(entry.links) > 0 {
			result = append(result, entry)
		}
	}
	return result, nil
}

// Only a link with an original effective binding can become targetless. A
// surviving primary or additional action can still make the link actionable.
func linkHasBinding(s *model.Context, link types.Dict, site sourceSite) (bool, error) {
	dest, err := navigationValue(s, link["Dest"], site.key("Dest"))
	if err != nil || dest != nil {
		return dest != nil, err
	}
	hasAction := func(raw types.Object, site sourceSite) (bool, error) {
		value, err := navigationValue(s, raw, site)
		switch v := value.(type) {
		case types.Array:
			return len(v) > 0, err
		case types.Dict:
			return v["S"] != nil, err
		default:
			return value != nil, err
		}
	}
	action, err := hasAction(link["A"], site.key("A"))
	if err != nil || action {
		return action, err
	}
	additional, err := navigationContainer[types.Dict](s, link["AA"], site.key("AA"))
	if err != nil {
		return false, err
	}
	for _, key := range slices.Sorted(maps.Keys(additional)) {
		if action, err := hasAction(additional[key], site.key("AA").key(key)); err != nil || action {
			return action, err
		}
	}
	return false, nil
}

func pruneProjectedLinks(c context.Context, s *model.Context, pages []projectedLinkPage) error {
	removed := map[types.IndirectRef]bool{}
	for _, page := range pages {
		if err := c.Err(); err != nil {
			return err
		}
		var dropped map[int]bool
		for _, link := range page.links {
			kept, err := linkHasBinding(s, link.dict, link.site)
			if err != nil {
				return err
			}
			if !kept {
				if dropped == nil {
					dropped = map[int]bool{}
				}
				dropped[link.index] = true
				if ref, ok := page.array[link.index].(types.IndirectRef); ok {
					removed[ref] = true
				}
			}
		}
		if len(dropped) == 0 {
			continue
		}
		kept := make(types.Array, 0, len(page.array)-len(dropped))
		for i, object := range page.array {
			if !dropped[i] {
				kept = append(kept, object)
			}
		}
		if len(kept) == 0 {
			delete(page.page, "Annots")
		} else {
			page.page["Annots"] = kept
		}
	}
	if len(removed) == 0 {
		return nil
	}
	root, err := structureRoot(s)
	if err != nil || root == nil {
		return err
	}
	// The existing structure composition pass rebuilds the reverse index.
	return structureWalk(c, s, root, func(d types.Dict, kind string, object, _ types.Object) (types.Object, error) {
		ref, ok := d["Obj"].(types.IndirectRef)
		if kind == "OBJR" && ok && removed[ref] {
			return nil, nil
		}
		return object, nil
	})
}
