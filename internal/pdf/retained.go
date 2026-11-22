// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"slices"

	"github.com/resoltico/pdfconcat/internal/core/diag"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type retainedTarget struct {
	owner      types.Dict
	key        string
	value      types.Object
	site, edge sourceSite
}

// Resolve the owning slot before observers or the backend can interpret it.
func visitRetainedTarget(s *model.Context, d types.Dict, key string, site, edge sourceSite, visit func(retainedTarget) (bool, error)) (bool, error) {
	value, err := navigationValue(s, d[key], site)
	if err != nil {
		return false, err
	}
	if key == "D" {
		switch v := value.(type) {
		case types.Name, types.StringLiteral, types.HexLiteral:
		case types.Array:
			if len(v) == 0 {
				return false, site.wrap(refuse(diag.SourceUnsupported, diag.InvalidLocalGoToDestination()))
			}
		default:
			return false, site.wrap(refuse(diag.SourceUnsupported, diag.InvalidLocalGoToDestination()))
		}
	}
	return visit(retainedTarget{d, key, value, site, edge})
}

// retainedObjects follows semantic edges that survive catalog cleanup. Private
// dictionary entries cannot establish annotation, field or navigation roles.
// Contexts have already passed read's structural checks. This observer is
// iterative: each indirect object is visited once per role and direct containers
// are finite parsed trees, so array wrappers and role transitions need no extra
// nesting limit. Existing read-time limits remain the source policy.
func retainedObjects(c context.Context, s *model.Context, annotation func(types.Dict) error, target func(types.Object) (bool, error)) error {
	origins, e := sourceOriginsFor(c, s)
	if e != nil {
		return e
	}
	return walkRetained(c, s, origins, func(d types.Dict, _ sourceSite) error { return annotation(d) }, func(binding retainedTarget) (bool, error) { return target(binding.value) }, nil, false)
}

// Inspect local operands with full object identity before semantic validation.
// Source normalization replaces only owned slots and leaves shared arrays intact.
func inspectRetainedDestinations(c context.Context, s *model.Context, pages []pageRef, normalize bool) error {
	origins := sourceOrigins{pages: make(map[types.IndirectRef]int, len(pages))}
	for i, page := range pages {
		origins.pages[page.ref] = i + 1
	}
	var edits []navigationSlot
	err := walkRetained(c, s, origins, nil, func(binding retainedTarget) (bool, error) {
		if binding.value == nil && normalize && binding.owner[binding.key] != nil {
			edits = append(edits, navigationSlot{binding.owner, binding.key, nil})
		}
		array, ok := binding.value.(types.Array)
		if !ok || len(array) == 0 {
			return true, nil
		}
		if err := checkExplicitStructureDestination(s, array, binding.site); err != nil {
			return false, err
		}
		ref, _ := array[0].(types.IndirectRef)
		if origins.pages[ref] == 0 {
			return true, nil
		}
		dest, err := readExplicitDestination(s, array)
		if err != nil {
			return false, binding.site.wrap(err)
		}
		if normalize && dest.hasIndirectOperands {
			edits = append(edits, navigationSlot{binding.owner, binding.key, dest.array()})
		}
		return true, nil
	}, nil, false)
	if err != nil {
		return err
	}
	for _, edit := range edits {
		if edit.value == nil {
			delete(edit.owner, edit.key)
		} else {
			edit.owner[edit.key] = edit.value
		}
	}
	return nil
}

// The worklist carries original enclosing addresses. Edits are applied after
// observation, so shared routes see the same input and dependent actions are
// pruned without obscuring another incoming edge's evidence.
func walkRetained(c context.Context, s *model.Context, origins sourceOrigins, annotation func(types.Dict, sourceSite) error, target func(retainedTarget) (bool, error), action func(types.Dict, sourceSite, types.Name) error, prune bool) error {
	type visit struct {
		object     types.Object
		role       string
		site, edge sourceSite
		replace    func(types.Object)
	}
	type node struct {
		ref  types.IndirectRef
		role string
		page int
	}
	type actionArrayEdit struct {
		items   types.Array
		owners  []func(types.Object)
		changed bool
	}
	cat, e := s.Catalog()
	if e != nil {
		return e
	}
	base := catalogSite(s)
	form, e := navigationContainer[types.Dict](s, cat["AcroForm"], base.key("AcroForm"))
	if e != nil {
		return e
	}
	formSite := base.key("AcroForm").anchor(cat["AcroForm"], origins)
	outline, e := navigationContainer[types.Dict](s, cat["Outlines"], base.key("Outlines"))
	if e != nil {
		return e
	}
	outlineSite := base.key("Outlines").anchor(cat["Outlines"], origins)
	if _, e := navigationValue(s, outline["Last"], outlineSite.key("Last")); e != nil {
		return e
	}
	pending := []visit{{object: outline["First"], role: "outlines", site: outlineSite.key("First")}, {object: form["Fields"], role: "fields", site: formSite.key("Fields")}, {object: cat["Pages"], role: "pages", site: base.key("Pages")}}
	var edits, cleanups []func()
	if prune && form == nil && cat["AcroForm"] != nil {
		edits = append(edits, func() { delete(cat, "AcroForm") })
	}
	seen := map[node]bool{}
	arrayEdits := map[node]*actionArrayEdit{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if e := c.Err(); e != nil {
			return e
		}
		if current.object == nil {
			continue
		}
		site := current.site.anchor(current.object, origins)
		value, e := navigationValue(s, current.object, current.site)
		if e != nil {
			return e
		}
		if value == nil && prune && current.replace != nil && current.role != "pages" {
			edits = append(edits, func() { current.replace(nil) })
			continue
		}
		var actionKind types.Name
		if current.role == "action" {
			if d, ok := value.(types.Dict); ok {
				kind, e := navigationValue(s, d["S"], site.key("S"))
				if e != nil {
					return e
				}
				actionKind, _ = kind.(types.Name)
				if actionKind == "GoTo" {
					if raw := d["SD"]; raw != nil && (s.PDF20() || prune) {
						absent, err := absentStructureDestination(s, raw, site.key("SD"))
						if err != nil {
							return err
						}
						if absent && prune {
							edits = append(edits, func() { delete(d, "SD") })
						}
					}
					keep, e := visitRetainedTarget(s, d, "D", site.key("D"), current.edge, target)
					if e != nil {
						return e
					}
					if !keep {
						if prune && current.replace != nil {
							replace := current.replace
							edits = append(edits, func() { replace(nil) })
						}
						continue
					}
				}
			}
		}
		ref, indirect := current.object.(types.IndirectRef)
		identity := node{ref, current.role, site.page}
		if indirect {
			if seen[identity] {
				if edit := arrayEdits[identity]; edit != nil && current.replace != nil {
					edit.owners = append(edit.owners, current.replace)
				}
				continue
			}
			seen[identity] = true
		}
		if array, ok := value.(types.Array); ok {
			var edit *actionArrayEdit
			if prune && current.role == "action" && current.replace != nil {
				// An action array can belong to several semantic owners or
				// also be referenced by a private edge. Project only the
				// participating slots, preserving every independent sibling.
				edit = &actionArrayEdit{items: slices.Clone(array), owners: []func(types.Object){current.replace}}
				if indirect {
					arrayEdits[identity] = edit
				}
				cleanups = append(cleanups, func() {
					kept := slices.DeleteFunc(edit.items, func(item types.Object) bool { return item == nil })
					if !edit.changed && len(kept) == len(array) && len(kept) > 0 {
						return
					}
					for _, replace := range edit.owners {
						if len(kept) == 0 {
							replace(nil)
						} else {
							replace(kept)
						}
					}
				})
			}
			for i := len(array) - 1; i >= 0; i-- {
				childSite := site.index(i)
				var replace func(types.Object)
				if edit != nil {
					replace = func(o types.Object) {
						edit.items[i] = o
						edit.changed = true
					}
				}
				pending = append(pending, visit{object: array[i], role: current.role, site: childSite, edge: childSite, replace: replace})
			}
			continue
		}
		d, ok := value.(types.Dict)
		if !ok {
			continue
		}
		follow := func(key, role string) {
			edge := site.key(key)
			pending = append(pending, visit{object: d[key], role: role, site: edge, edge: edge, replace: func(o types.Object) {
				if o == nil {
					delete(d, key)
				} else {
					d[key] = o
				}
			}})
		}
		push := func(o types.Object, role string, where sourceSite) {
			pending = append(pending, visit{object: o, role: role, site: where})
		}
		var subtype types.Object
		if current.role == "annotation" || current.role == "fields" {
			subtype, e = navigationValue(s, d["Subtype"], site.key("Subtype"))
			if e != nil {
				return e
			}
		}
		switch current.role {
		case "pages":
			kind, e := navigationValue(s, d["Type"], site.key("Type"))
			if e != nil {
				return e
			}
			if kind != types.Name("Page") {
				follow("Kids", "pages")
				continue
			}
			follow("AA", "additionalActions")
			follow("Annots", "annotation")
		case "fields":
			follow("Kids", "fields")
			if subtype == types.Name("Widget") {
				push(current.object, "annotation", site)
			} else {
				follow("AA", "additionalActions")
			}
		case "annotation":
			if annotation != nil {
				if e := annotation(d, site); e != nil {
					return e
				}
			}
			for _, key := range []string{"Popup", "IRT"} {
				follow(key, "annotation")
			}
			if subtype == types.Name("Popup") {
				follow("Parent", "annotation")
			}
			if subtype == types.Name("Link") {
				keep, e := visitRetainedTarget(s, d, "Dest", site.key("Dest"), sourceSite{}, target)
				if e != nil {
					return e
				}
				if !keep && prune {
					edits = append(edits, func() { delete(d, "Dest") })
				}
			}
			if subtype == types.Name("Link") || subtype == types.Name("Widget") || subtype == types.Name("Screen") {
				follow("A", "action")
			}
			follow("AA", "additionalActions")
		case "outlines":
			for _, key := range []string{"Parent", "Prev", "Last"} {
				if _, e := navigationValue(s, d[key], site.key(key)); e != nil {
					return e
				}
			}
			keep, e := visitRetainedTarget(s, d, "Dest", site.key("Dest"), sourceSite{}, target)
			if e != nil {
				return e
			}
			if !keep && prune {
				edits = append(edits, func() { delete(d, "Dest") })
			}
			follow("A", "action")
			push(d["Next"], "outlines", site.key("Next"))
			follow("First", "outlines")
		case "additionalActions":
			keys := make([]string, 0, len(d))
			for key := range d {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for i := len(keys) - 1; i >= 0; i-- {
				follow(keys[i], "action")
			}
		case "action":
			if action != nil {
				if e := action(d, site, actionKind); e != nil {
					return e
				}
			}
			follow("Next", "action")
		}
	}
	for _, edit := range edits {
		edit()
	}
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
	return nil
}

// appearanceIssue delegates the exception rules to the pinned writer's public
// checker. Only its missing-appearance failure is a compatibility fact; graph,
// dereference and cancellation failures must still fail inspection.
func appearanceIssue(c context.Context, s *model.Context) (diag.AnnotationIssue, error) {
	header, root := s.HeaderVersion, s.RootVersion
	defer func() { s.HeaderVersion = header; s.RootVersion = root }()
	version := model.V20
	s.HeaderVersion = &version
	s.RootVersion = &version
	issue := diag.AnnotationIssue{}
	origins, e := sourceOriginsFor(c, s)
	if e != nil {
		return issue, e
	}
	err := walkRetained(c, s, origins, func(d types.Dict, site sourceSite) error {
		observed, e := appearanceCompatibilityFact(s.CheckAnnotationAppearance(d))
		if e != nil {
			return e
		}
		if !issue.Present() && observed.Present() {
			issue = observed
			if location := site.location(); location != nil {
				issue.Site = *location
				issue.Page = location.Page
			}
		}

		return nil
	}, func(retainedTarget) (bool, error) { return true, nil }, nil, false)
	return issue, err
}

// appearanceCompatibilityFact selects a typed category, never its rendering.
func appearanceCompatibilityFact(err error) (diag.AnnotationIssue, error) {
	if err == nil {
		return diag.AnnotationIssue{}, nil
	}
	var missing *model.MissingAnnotationAppearanceError
	if !errors.As(err, &missing) {
		return diag.AnnotationIssue{}, err
	}
	return diag.AnnotationIssue{Kind: diag.PDF20AppearanceMissing, Subtype: missing.Subtype}, nil
}
