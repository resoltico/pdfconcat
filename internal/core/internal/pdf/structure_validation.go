// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

func numberTree(c context.Context, s *model.Context, o types.Object) (map[int]types.Object, error) {
	entries, err := readNumberTree(c, s, o, diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "NumberTree"})
	out := map[int]types.Object{}
	for key, entry := range entries {
		out[key] = entry.value
	}
	return out, err
}

// validateStructure verifies both directions of source semantic ownership before
// selection or fitting can discard evidence of a malformed original tree.
func validateStructure(c context.Context, s *model.Context) error {
	return validateStructureGraph(c, s, nil)
}

func validateStructureGraph(c context.Context, s *model.Context, normal *structureNormalization) (err error) {
	loc := diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog.StructTreeRoot"}
	defer func() { err = structureAt(err, loc) }()
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
			for _, key := range []string{"StructParent", "StructParents"} {
				value, e := s.Dereference(d[key])
				if e != nil {
					return e
				}
				if value != nil {
					return errors.New("source has semantic ownership without a structure tree")
				}
			}
			return nil
		})
	}
	if ref, ok := cat["StructTreeRoot"].(types.IndirectRef); ok {
		loc.Object = ref.ObjectNumber.Value()
	}
	rootObject := loc.Object
	loc.Path = "StructTreeRoot.Namespaces"
	if err = validateNamespaces(c, s, root); err != nil {
		return err
	}
	loc.Path = "StructTreeRoot.RoleMap/ClassMap"
	if err = validateStructureBindings(c, s, root); err != nil {
		return err
	}
	if err = validateRoleMap(s, root); err != nil {
		return err
	}
	loc.Path = "StructTreeRoot.IDTree"
	idTree, err := structureIDTree(c, s, root["IDTree"])
	if err != nil {
		return err
	}
	loc.Path = "StructTreeRoot.K/MarkedContent"
	marked, err := markedContent(c, s)
	if err != nil {
		return err
	}
	loc.Path = "StructTreeRoot.ParentTree"
	entries, err := readNumberTree(c, s, root["ParentTree"], loc)
	if err != nil {
		return err
	}
	inverse := map[int]types.Object{}
	for key, entry := range entries {
		inverse[key] = entry.value
	}
	claims, err := structureClaims(c, s)
	if err != nil {
		return err
	}
	for key, claim := range claims {
		if _, ok := inverse[key]; !ok {
			return structureAt(errors.New("structure owner key is absent from ParentTree"), claim)
		}
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

		if ref, ok := o.(types.IndirectRef); ok {
			loc.Object = ref.ObjectNumber.Value()
		}
		loc.Path = "StructTreeRoot.K"
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
				if kind, e := nameValue(s, d["Type"]); e != nil || (kind != "" && kind != "StructElem") {
					return errors.New("structure element Type must be StructElem")
				}
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
		loc.Object = ref.ObjectNumber.Value()
		loc.Path = fmt.Sprintf("Object.%s(key=%d); parent element %s", keyName, key.Value(), parent)
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
	keys := make([]int, 0, len(inverse))
	for key := range inverse {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		value := inverse[key]
		loc = entries[key].location
		v, e := s.Dereference(value)
		if e != nil {
			return e
		}
		if a, ok := v.(types.Array); ok {
			for i, parent := range a {
				resolved, e := s.Dereference(parent)
				if e != nil {
					return e
				}
				if resolved != nil && !sameRef(used[key][i], parent) {
					return errors.New("parent tree contains unreferenced semantic content")
				}
			}
		} else if v != nil && !sameRef(used[key][-1], value) {
			ref, ok := value.(types.IndirectRef)
			if normal == nil || claims[key].Object != 0 || !ok || !seen[ref.ObjectNumber.Value()] {
				return errors.New("parent tree contains unreferenced object ownership")
			}
			if normal.stale == nil {
				normal.stale = map[int]numberEntry{}
			}
			normal.stale[key] = entries[key]
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
	loc = diag.PDFLocation{Object: rootObject, Path: "StructTreeRoot.IDTree"}
	if normal != nil && root["IDTree"] == nil && len(ids) > 0 {
		normal.ids = ids
		idTree = ids
	}
	if len(ids) != len(idTree) {
		return errors.New("structure IDs and ID tree differ")
	}
	for id, ref := range ids {
		if !sameRef(ref, idTree[id]) {
			return errors.New("structure ID tree points to the wrong element")
		}
	}
	loc.Path = "StructTreeRoot.K.Headers"
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
