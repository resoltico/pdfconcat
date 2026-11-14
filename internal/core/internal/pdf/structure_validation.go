// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func numberTree(c context.Context, s *model.Context, o types.Object) (map[int]types.Object, error) {
	out := map[int]types.Object{}
	seen := map[int]bool{}
	var walk func(types.Object, int) error
	walk = func(o types.Object, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 256 {
			return errors.New("number tree nesting exceeds 256")
		}
		if ref, ok := o.(types.IndirectRef); ok {
			n := ref.ObjectNumber.Value()
			if seen[n] {
				return errors.New("cyclic number tree")
			}
			seen[n] = true
		}
		d, err := s.DereferenceDict(o)
		if err != nil {
			return err
		}
		if d == nil {
			return nil
		}
		nums, err := s.DereferenceArray(d["Nums"])
		if err != nil {
			return err
		}
		if len(nums)%2 != 0 {
			return errors.New("odd number tree Nums")
		}
		for i := 0; i < len(nums); i += 2 {
			n, e := s.DereferenceInteger(nums[i])
			if e != nil || n == nil || n.Value() < 0 {
				return errors.New("invalid number tree index")
			}
			if _, found := out[n.Value()]; found {
				return errors.New("duplicate number tree index")
			}
			out[n.Value()] = nums[i+1]
		}
		kids, err := s.DereferenceArray(d["Kids"])
		if err != nil {
			return err
		}
		for _, kid := range kids {
			if err = walk(kid, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(o, 0)
	return out, err
}

// validateStructure verifies both directions of source semantic ownership before
// selection or fitting can discard evidence of a malformed original tree.
func validateStructure(c context.Context, s *model.Context) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	root, err := structureRoot(s)
	if err != nil {
		return err
	}
	if root == nil {
		marked, e := s.DereferenceDict(cat["MarkInfo"])
		if e != nil {
			return e
		}
		if marked["Marked"] == types.Boolean(true) {
			return errors.New("marked source lacks a structure tree")
		}
		return walkObjects(c, s, func(d types.Dict) error {
			if d["StructParent"] != nil || d["StructParents"] != nil {
				return errors.New("source has semantic ownership without a structure tree")
			}
			return nil
		})
	}
	if err = validateNamespaces(c, s, root); err != nil {
		return err
	}
	if err = validateStructureBindings(c, s, root); err != nil {
		return err
	}
	if err = validateRoleMap(s, root); err != nil {
		return err
	}
	idTree, err := structureIDTree(c, s, root["IDTree"])
	if err != nil {
		return err
	}
	marked, err := markedContent(c, s)
	if err != nil {
		return err
	}
	inverse, err := numberTree(c, s, root["ParentTree"])
	if err != nil {
		return err
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return err
	}
	pageIDs := map[int]bool{}
	annotationPages := map[types.IndirectRef]map[types.IndirectRef]bool{}
	for _, p := range pages {
		pageIDs[p.ref.ObjectNumber.Value()] = true
		annotations, e := s.DereferenceArray(p.dict["Annots"])
		if e != nil {
			return e
		}
		for _, object := range annotations {
			if ref, ok := object.(types.IndirectRef); ok {
				if annotationPages[ref] == nil {
					annotationPages[ref] = map[types.IndirectRef]bool{}
				}
				annotationPages[ref][p.ref] = true
			}
		}
	}
	seen := map[int]bool{}
	ids := map[string]types.Object{}
	used := map[int]map[int]types.Object{}
	var walk func(types.Object, types.Object, types.Object, int) error
	walk = func(o, parent, page types.Object, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 256 {
			return errors.New("structure nesting exceeds 256")
		}
		v, e := s.Dereference(o)
		if e != nil {
			return e
		}
		if a, ok := v.(types.Array); ok {
			for _, child := range a {
				if e = walk(child, parent, page, depth+1); e != nil {
					return e
				}
			}
			return nil
		}
		if v == nil {
			return nil
		}
		var d types.Dict
		var mcid *types.Integer
		var owner types.Object
		switch x := v.(type) {
		case types.Integer:
			mcid = &x
			owner = page
		case types.Dict:
			d = x
			if d["Pg"] != nil {
				page = d["Pg"]
			}
			if d["S"] != nil {
				ref, ok := o.(types.IndirectRef)
				if !ok {
					return errors.New("structure element must be indirect")
				}
				n := ref.ObjectNumber.Value()
				if seen[n] {
					return errors.New("cyclic or multiply-owned structure element")
				}
				seen[n] = true
				if !sameRef(d["P"], parent) {
					return errors.New("structure parent/child references disagree")
				}
				if id := d["ID"]; id != nil {
					text, e := structureID(s, id)
					if e != nil {
						return e
					}
					if ids[text] != nil {
						return errors.New("duplicate structure ID")
					}
					ids[text] = o
				}
				return walk(d["K"], o, page, depth+1)
			}
			typ, e := nameValue(s, d["Type"])
			if e != nil {
				return e
			}
			switch typ {
			case "MCR":
				mcid, e = s.DereferenceInteger(d["MCID"])
				if e != nil || mcid == nil {
					return errors.New("MCR lacks integer MCID")
				}
				owner = page
				if d["Stm"] != nil {
					owner = d["Stm"]
				}
			case "OBJR":
				owner = d["Obj"]
			default:
				return errors.New("unsupported structure child dictionary")
			}
		default:
			return fmt.Errorf("invalid structure child %T", v)
		}
		pg, ok := page.(types.IndirectRef)
		if !ok || !pageIDs[pg.ObjectNumber.Value()] {
			return errors.New("structure content references an absent page")
		}
		ref, ok := owner.(types.IndirectRef)
		if !ok {
			return errors.New("structure content lacks indirect owner")
		}
		value, e := s.Dereference(ref)
		if e != nil {
			return e
		}
		var container types.Dict
		switch x := value.(type) {
		case types.Dict:
			container = x
			if mcid == nil && !annotationPages[ref][pg] {
				return errors.New("object reference annotation is not on its declared page")
			}
		case types.StreamDict:
			container = x.Dict
		default:
			return errors.New("invalid structure content owner")
		}
		keyName := "StructParent"
		if mcid != nil {
			keyName = "StructParents"
		}
		key, e := s.DereferenceInteger(container[keyName])
		if e != nil || key == nil || key.Value() < 0 {
			return errors.New("structure owner lacks valid parent-tree key")
		}
		entry := inverse[key.Value()]
		index := -1
		if mcid != nil {
			index = mcid.Value()
			if !marked.ids[ref.ObjectNumber.Value()][index] {
				return errors.New("structure MCID is absent from its content stream")
			}
			if index < 0 || index > 1_000_000 {
				return errors.New("MCID out of supported bounds")
			}
			a, e := s.DereferenceArray(entry)
			if e != nil || index >= len(a) {
				return errors.New("MCID is absent from parent tree")
			}
			entry = a[index]
		}
		if !sameRef(entry, parent) {
			return errors.New("parent tree disagrees with semantic content owner")
		}
		if used[key.Value()] == nil {
			used[key.Value()] = map[int]types.Object{}
		}
		if used[key.Value()][index] != nil {
			return errors.New("semantic content has multiple owners")
		}
		used[key.Value()][index] = parent
		return nil
	}
	if err = walk(root["K"], cat["StructTreeRoot"], nil, 0); err != nil {
		return err
	}
	for key, value := range inverse {
		v, e := s.Dereference(value)
		if e != nil {
			return e
		}
		if a, ok := v.(types.Array); ok {
			for i, parent := range a {
				if parent != nil && !sameRef(used[key][i], parent) {
					return errors.New("parent tree contains unreferenced semantic content")
				}
			}
		} else if value != nil && !sameRef(used[key][-1], value) {
			return errors.New("parent tree contains unreferenced object ownership")
		}
	}
	if err = structureElements(c, s, root["K"], nil, func(d types.Dict, _ types.Object) error {
		refs, e := s.DereferenceArray(d["Ref"])
		if e != nil {
			return e
		}
		for _, value := range refs {
			ref, ok := value.(types.IndirectRef)
			if !ok || !seen[ref.ObjectNumber.Value()] {
				return errors.New("structure Ref points outside the structure hierarchy")
			}
		}
		return nil
	}, 0); err != nil {
		return err
	}
	if len(ids) != len(idTree) {
		return errors.New("structure IDs and ID tree differ")
	}
	for id, ref := range ids {
		if !sameRef(ref, idTree[id]) {
			return errors.New("structure ID tree points to the wrong element")
		}
	}
	return validateTableHeaders(c, s, root)
}

func structureIDTree(c context.Context, s *model.Context, o types.Object) (map[string]types.Object, error) {
	out := map[string]types.Object{}
	seen := map[int]bool{}
	var walk func(types.Object, int) error
	walk = func(o types.Object, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 256 {
			return errors.New("ID tree nesting exceeds 256")
		}
		if ref, ok := o.(types.IndirectRef); ok {
			n := ref.ObjectNumber.Value()
			if seen[n] {
				return errors.New("cyclic ID tree")
			}
			seen[n] = true
		}
		d, err := s.DereferenceDict(o)
		if err != nil {
			return err
		}
		if d == nil {
			return nil
		}
		names, err := s.DereferenceArray(d["Names"])
		if err != nil {
			return err
		}
		if len(names)%2 != 0 {
			return errors.New("odd ID tree names")
		}
		for i := 0; i < len(names); i += 2 {
			key, e := structureID(s, names[i])
			if e != nil {
				return e
			}
			if out[key] != nil {
				return errors.New("duplicate ID tree key")
			}
			out[key] = names[i+1]
		}
		kids, err := s.DereferenceArray(d["Kids"])
		if err != nil {
			return err
		}
		for _, kid := range kids {
			if err = walk(kid, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(o, 0)
	return out, err
}

func validateRoleMap(s *model.Context, root types.Dict) error {
	roles, err := s.DereferenceDict(root["RoleMap"])
	if err != nil {
		return err
	}
	for key := range roles {
		seen := map[string]bool{}
		name := key
		for roles[name] != nil {
			if seen[name] {
				return errors.New("cyclic structure role map")
			}
			seen[name] = true
			value, err := nameValue(s, roles[name])
			if err != nil {
				return err
			}
			name = string(value)
		}
	}
	return nil
}
