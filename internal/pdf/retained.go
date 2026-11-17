// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"slices"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

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
	return walkRetained(c, s, origins, func(d types.Dict, _ sourceSite) error { return annotation(d) }, func(d types.Dict, key string, _ sourceSite, _ sourceSite) (bool, error) { return target(d[key]) }, nil, false)
}

// The worklist carries original enclosing addresses. Edits are applied after
// observation, so shared routes see the same input and dependent actions are
// pruned without obscuring another incoming edge's evidence.
func walkRetained(c context.Context, s *model.Context, origins sourceOrigins, annotation func(types.Dict, sourceSite) error, target func(types.Dict, string, sourceSite, sourceSite) (bool, error), action func(types.Dict, sourceSite) error, prune bool) error {
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
	cat, e := s.Catalog()
	if e != nil {
		return e
	}
	base := catalogSite(s)
	form, e := s.DereferenceDict(cat["AcroForm"])
	if e != nil {
		return e
	}
	formSite := base.key("AcroForm").anchor(cat["AcroForm"], origins)
	outline, e := s.DereferenceDict(cat["Outlines"])
	if e != nil {
		return e
	}
	outlineSite := base.key("Outlines").anchor(cat["Outlines"], origins)
	pending := []visit{{object: outline["First"], role: "outlines", site: outlineSite.key("First")}, {object: form["Fields"], role: "fields", site: formSite.key("Fields")}, {object: cat["Pages"], role: "pages", site: base.key("Pages")}}
	var edits, cleanups []func()
	seen := map[node]bool{}
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
		value, e := s.Dereference(current.object)
		if e != nil {
			return e
		}
		if current.role == "action" {
			if d, ok := value.(types.Dict); ok {
				kind, e := nameValue(s, d["S"])
				if e != nil {
					return e
				}
				if kind == "GoTo" {
					keep, e := target(d, "D", site.key("D"), current.edge)
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
		if ref, ok := current.object.(types.IndirectRef); ok {
			key := node{ref, current.role, site.page}
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		if array, ok := value.(types.Array); ok {
			for i := len(array) - 1; i >= 0; i-- {
				childSite := site.index(i)
				pending = append(pending, visit{object: array[i], role: current.role, site: childSite, edge: childSite, replace: func(o types.Object) { array[i] = o }})
			}
			if prune && current.role == "action" && current.replace != nil {
				replace, object := current.replace, current.object
				cleanups = append(cleanups, func() {
					kept := types.Array{}
					for _, child := range array {
						if child != nil {
							kept = append(kept, child)
						}
					}
					if ref, ok := object.(types.IndirectRef); ok {
						s.Table[ref.ObjectNumber.Value()].Object = kept
					}
					if len(kept) == 0 {
						replace(nil)
					} else {
						replace(kept)
					}
				})
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
		subtype, e := nameValue(s, d["Subtype"])
		if e != nil {
			return e
		}
		switch current.role {
		case "pages":
			kind, e := nameValue(s, d["Type"])
			if e != nil {
				return e
			}
			if kind != "Page" {
				follow("Kids", "pages")
				continue
			}
			follow("AA", "additionalActions")
			follow("Annots", "annotation")
		case "fields":
			follow("Kids", "fields")
			if subtype == "Widget" {
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
			if subtype == "Popup" {
				follow("Parent", "annotation")
			}
			if subtype == "Link" {
				keep, e := target(d, "Dest", site.key("Dest"), sourceSite{})
				if e != nil {
					return e
				}
				if !keep && prune {
					edits = append(edits, func() { delete(d, "Dest") })
				}
			}
			if subtype == "Link" || subtype == "Widget" || subtype == "Screen" {
				follow("A", "action")
			}
			follow("AA", "additionalActions")
		case "outlines":
			keep, e := target(d, "Dest", site.key("Dest"), sourceSite{})
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
				if e := action(d, site); e != nil {
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

func numericPosition(tail types.Array) bool {
	if len(tail) == 0 {
		return false
	}
	slots := []int{}
	switch tail[0] {
	case types.Name("XYZ"):
		slots = []int{1, 2}
	case types.Name("FitH"), types.Name("FitBH"), types.Name("FitV"), types.Name("FitBV"):
		slots = []int{1}
	case types.Name("FitR"):
		slots = []int{1, 2, 3, 4}
	}
	for _, i := range slots {
		if i < len(tail) {
			switch tail[i].(type) {
			case types.Integer, types.Float:
				return true
			}
		}
	}
	return false
}

func coordinateDestinations(c context.Context, s *model.Context, named map[string]destination) (bool, error) {
	found := false
	for _, d := range named {
		if d.page > 0 && numericPosition(d.tail) {
			found = true
		}
	}
	err := retainedObjects(c, s, func(types.Dict) error { return nil }, func(o types.Object) (bool, error) {
		value, err := s.Dereference(o)
		if err != nil {
			return false, err
		}
		if name, e := destinationName(s, value); e == nil {
			if d, ok := named[name]; ok && d.page < 1 {
				return false, nil
			}
		}
		a, ok := value.(types.Array)
		if !ok || len(a) < 2 {
			return true, nil
		}
		tail := make(types.Array, 0, len(a)-1)
		for _, entry := range a[1:] {
			v, err := s.Dereference(entry)
			if err != nil {
				return false, err
			}
			tail = append(tail, v)
		}
		found = found || numericPosition(tail)
		return true, nil
	})
	return found, err
}

// appearanceIssue delegates the exception rules to the pinned writer's public
// checker. Only its missing-appearance failure is a compatibility fact; graph,
// dereference and cancellation failures must still fail inspection.
func appearanceIssue(c context.Context, s *model.Context) (string, error) {
	header, root := s.HeaderVersion, s.RootVersion
	defer func() { s.HeaderVersion = header; s.RootVersion = root }()
	version := model.V20
	s.HeaderVersion = &version
	s.RootVersion = &version
	issue := ""
	err := retainedObjects(c, s, func(d types.Dict) error {
		if err := s.CheckAnnotationAppearance(d); err != nil {
			if !strings.Contains(err.Error(), "requires an appearance dictionary (AP)") {
				return err
			}
			if issue == "" {
				issue = err.Error()
			}
		}
		return nil
	}, func(types.Object) (bool, error) { return true, nil })
	return issue, err
}
