// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
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

	return walkRetained(c, s, annotation, func(d types.Dict, key string) (bool, error) { return target(d[key]) }, false)
}

func walkRetained(c context.Context, s *model.Context, annotation func(types.Dict) error, target func(types.Dict, string) (bool, error), prune bool) error {

	type visit struct {
		object  types.Object
		role    string
		replace func(types.Object)
	}
	type node struct {
		ref  types.IndirectRef
		role string
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	form, err := s.DereferenceDict(cat["AcroForm"])
	if err != nil {
		return err
	}
	outlines, err := s.DereferenceDict(cat["Outlines"])
	if err != nil {
		return err
	}
	pending := []visit{{object: outlines["First"], role: "outlines"}, {object: form["Fields"], role: "fields"}, {object: cat["Pages"], role: "pages"}}
	var cleanups []func()
	seen := map[node]bool{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if err := c.Err(); err != nil {
			return err
		}
		if current.object == nil {
			continue
		}
		value, err := s.Dereference(current.object)
		if err != nil {
			return err
		}
		if current.role == "action" {
			if d, ok := value.(types.Dict); ok {
				kind, e := nameValue(s, d["S"])
				if e != nil {
					return e
				}
				if kind == "GoTo" {
					keep, e := target(d, "D")
					if e != nil {
						return e
					}
					if !keep {
						if prune && current.replace != nil {
							current.replace(nil)
						}
						continue
					}
				}
			}
		}
		if ref, ok := current.object.(types.IndirectRef); ok {
			key := node{ref, current.role}
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		push := func(object types.Object, role string) {
			pending = append(pending, visit{object: object, role: role})
		}
		if array, ok := value.(types.Array); ok {
			for i := len(array) - 1; i >= 0; i-- {
				pending = append(pending, visit{object: array[i], role: current.role, replace: func(o types.Object) { array[i] = o }})
			}
			if prune && current.role == "action" && current.replace != nil {
				replace := current.replace
				object := current.object
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
			pending = append(pending, visit{object: d[key], role: role, replace: func(o types.Object) {
				if o == nil {
					delete(d, key)
				} else {
					d[key] = o
				}
			}})
		}
		subtype, err := nameValue(s, d["Subtype"])
		if err != nil {
			return err
		}
		switch current.role {
		case "pages":
			kind, err := nameValue(s, d["Type"])
			if err != nil {
				return err
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
				push(current.object, "annotation")
			} else {
				follow("AA", "additionalActions")
			}
		case "annotation":
			if err := annotation(d); err != nil {
				return err
			}
			for _, key := range []string{"Popup", "IRT"} {
				follow(key, "annotation")
			}
			if subtype == "Popup" {
				follow("Parent", "annotation")
			}
			if subtype == "Link" {
				if keep, err := target(d, "Dest"); err != nil {
					return err
				} else if !keep && prune {
					delete(d, "Dest")
				}
			}
			// A is defined for Link, Widget and Screen annotations. A key on other
			// annotation subtypes does not establish a navigation role.
			if subtype == "Link" || subtype == "Widget" || subtype == "Screen" {
				follow("A", "action")
			}
			follow("AA", "additionalActions")
		case "outlines":
			if keep, err := target(d, "Dest"); err != nil {
				return err
			} else if !keep && prune {
				delete(d, "Dest")
			}
			follow("A", "action")
			// Child and sibling edges use the worklist; traversing an accepted
			// outline adds no independent hierarchy limit.
			push(d["Next"], "outlines")
			follow("First", "outlines")
		case "additionalActions":
			for key := range d {
				follow(key, "action")
			}
		case "action":
			follow("Next", "action")
		}
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
