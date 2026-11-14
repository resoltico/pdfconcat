// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/matrix"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
)

// structureWalk visits semantic children with their inherited physical page.
// A structure element must have one owner; cycles and multiply-owned children
// are rejected rather than repaired into a different reading order.
func structureWalk(c context.Context, s *model.Context, root types.Dict, leaf func(types.Dict, string, types.Object, types.Object) (types.Object, error)) error {
	seen := map[int]bool{}
	var walk func(types.Object, types.Object, int) (types.Object, error)
	walk = func(o, page types.Object, depth int) (types.Object, error) {
		if err := c.Err(); err != nil {
			return nil, err
		}
		if depth > 256 {
			return nil, errors.New("structure nesting exceeds 256")
		}
		if ref, ok := o.(types.IndirectRef); ok {
			n := ref.ObjectNumber.Value()
			if seen[n] {
				return nil, errors.New("cyclic or multiply-owned structure child")
			}
			seen[n] = true
		}
		v, err := s.Dereference(o)
		if err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case nil:
			return nil, nil
		case types.Array:
			out := types.Array{}
			for _, child := range x {
				y, e := walk(child, page, depth+1)
				if e != nil {
					return nil, e
				}
				if y != nil {
					out = append(out, y)
				}
			}
			return out, nil
		case types.Integer:
			return leaf(nil, "MCID", x, page)
		case types.Dict:
			if p := x["Pg"]; p != nil {
				page = p
			}
			kind, _ := nameValue(s, x["Type"])
			if kind == "MCR" || kind == "OBJR" {
				return leaf(x, string(kind), o, page)
			}
			if x["S"] == nil {
				return nil, errors.New("structure child lacks semantic role")
			}
			children, e := walk(x["K"], page, depth+1)
			if e != nil {
				return nil, e
			}
			if a, ok := children.(types.Array); ok && len(a) == 0 {
				children = nil
			}
			if children == nil {
				delete(x, "K")
				delete(x, "Pg")
				return nil, nil
			}
			x["K"] = children
			return o, nil
		default:
			return nil, fmt.Errorf("unsupported structure child %T", v)
		}
	}
	children, err := walk(root["K"], nil, 0)
	if err != nil {
		return err
	}
	root["K"] = children
	return nil
}

func structureRoot(s *model.Context) (types.Dict, error) {
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	return s.DereferenceDict(cat["StructTreeRoot"])
}

// pruneStructure removes omitted semantic content before backend graph import.
func pruneStructure(c context.Context, s *model.Context) error {
	root, err := structureRoot(s)
	if err != nil || root == nil {
		return err
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return err
	}
	kept := map[int]bool{}
	for _, p := range pages {
		kept[p.ref.ObjectNumber.Value()] = true
	}
	err = structureWalk(c, s, root, func(d types.Dict, _ string, o, page types.Object) (types.Object, error) {
		ref, ok := page.(types.IndirectRef)
		if !ok {
			return nil, errors.New("structure content has no physical page")
		}
		if !kept[ref.ObjectNumber.Value()] {
			return nil, nil
		}
		if d == nil {
			return types.Dict{"Type": types.Name("MCR"), "Pg": page, "MCID": o}, nil
		}
		return o, nil
	})
	if err != nil {
		return err
	}
	if err = pruneStructurePages(c, s, root, kept); err != nil {
		return err
	}
	if err = validateTableHeaders(c, s, root); err != nil {
		return pdfFailure(diag.SourceUnsupported, "The selected structure cannot preserve table header associations. Include the referenced header pages, select the whole source, or explicitly discard structure.", err)
	}
	return nil
}

// fitStructure redirects page-owned marked content into the new Form's stream
// namespace. Existing nested Form ownership remains attached to that Form.
func fitStructure(c context.Context, s *model.Context, page types.IndirectRef, form types.IndirectRef, m matrix.Matrix) error {
	root, err := structureRoot(s)
	if err != nil || root == nil {
		return err
	}
	if err = fitStructureAttributes(c, s, root, page, m); err != nil {
		return err
	}
	return structureWalk(c, s, root, func(d types.Dict, kind string, o, pg types.Object) (types.Object, error) {
		if !sameRef(pg, page) {
			return o, nil
		}
		if kind == "OBJR" {
			return o, nil
		}
		if d == nil {
			return types.Dict{"Type": types.Name("MCR"), "Pg": page, "Stm": form, "MCID": o}, nil
		}
		if d["Stm"] == nil {
			d["Stm"] = form
		}
		return o, nil
	})
}

// structurePart remains a backend graph; no object identities escape to core.
type structurePart struct {
	catalog  types.Dict
	root     types.Dict
	language types.Object
}

// combineStructure gives every source occurrence its own subtree and namespace.
// ParentTree and IDTree are rebuilt from retained semantic ownership, rather
// than copying indexes that alias another source or retain selected-out text.
func combineStructure(c context.Context, s *model.Context, parts []structurePart) error {
	root := types.Dict{"Type": types.Name("StructTreeRoot")}
	rootRef, err := s.IndRefForNewObject(root)
	if err != nil {
		return err
	}
	document := types.Dict{"Type": types.Name("StructElem"), "S": types.Name("Document"), "P": *rootRef}
	documentRef, err := s.IndRefForNewObject(document)
	if err != nil {
		return err
	}
	kids := types.Array{}
	roles, classes := types.Dict{}, types.Dict{}
	namespaces := types.Array{}
	for i, part := range parts {
		if part.root == nil {
			continue
		}
		children, e := s.Dereference(part.root["K"])
		if e != nil {
			return e
		}
		if children == nil {
			continue
		}
		if a, ok := children.(types.Array); ok && len(a) == 0 {
			continue
		}
		prefix := fmt.Sprintf("PC%d_", i+1)
		rm, e := s.DereferenceDict(part.root["RoleMap"])
		if e != nil {
			return e
		}
		cm, e := s.DereferenceDict(part.root["ClassMap"])
		if e != nil {
			return e
		}
		for key, value := range rm {
			name, e := nameValue(s, value)
			if e != nil {
				return e
			}
			if rm[string(name)] != nil {
				name = types.Name(prefix + string(name))
			}
			roles[prefix+key] = name
		}
		for key, value := range cm {
			renamed, e := renameAttributeIDs(s, value, prefix)
			if e != nil {
				return e
			}
			classes[prefix+key] = renamed
		}
		ns, e := s.DereferenceArray(part.root["Namespaces"])
		if e != nil {
			return e
		}
		if e = renameNamespaceDefaultRoles(s, ns, prefix, rm); e != nil {
			return e
		}
		namespaces = append(namespaces, ns...)
		wrapper := types.Dict{"Type": types.Name("StructElem"), "S": types.Name("Part"), "P": *documentRef, "K": part.root["K"]}
		if part.catalog != nil {
			part.language = part.catalog["Lang"]
		}
		if part.language != nil {
			wrapper["Lang"] = part.language
		}
		ref, e := s.IndRefForNewObject(wrapper)
		if e != nil {
			return e
		}
		if err = renameStructure(c, s, wrapper, *ref, prefix, rm, cm); err != nil {
			return err
		}
		if wrapper["K"] != nil {
			kids = append(kids, *ref)
		}
	}
	document["K"] = kids
	root["K"] = *documentRef
	if len(roles) > 0 {
		root["RoleMap"] = roles
	}
	if len(classes) > 0 {
		root["ClassMap"] = classes
	}
	if len(namespaces) > 0 {
		root["Namespaces"] = namespaces
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	cat["StructTreeRoot"] = *rootRef
	cat["MarkInfo"] = types.Dict{"Marked": types.Boolean(true)}
	return rebuildStructureIndexes(c, s, root)
}

func renameStructure(c context.Context, s *model.Context, parent types.Dict, parentRef types.IndirectRef, prefix string, roles, classes types.Dict) error {
	var walk func(types.Object, types.IndirectRef, int) error
	walk = func(o types.Object, p types.IndirectRef, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 256 {
			return errors.New("structure nesting exceeds 256")
		}
		v, err := s.Dereference(o)
		if err != nil {
			return err
		}
		if a, ok := v.(types.Array); ok {
			for _, x := range a {
				if err = walk(x, p, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		d, ok := v.(types.Dict)
		if !ok {
			return nil
		}
		if d["S"] == nil {
			return nil
		}
		ref, ok := o.(types.IndirectRef)
		if !ok {
			return errors.New("direct structure elements are unsupported")
		}
		d["P"] = p
		role, e := nameValue(s, d["S"])
		if e != nil {
			return e
		}
		if d["NS"] == nil && roles[string(role)] != nil {
			role = types.Name(prefix + string(role))
		}
		d["S"] = role
		if d["C"] != nil {
			value, e := mapAttributeClasses(s, d["C"], func(name types.Name) (types.Object, error) {
				if classes[string(name)] == nil {
					return nil, errors.New("structure class is absent from ClassMap")
				}
				return types.Name(prefix + string(name)), nil
			}, 0)
			if e != nil {
				return e
			}
			d["C"] = value
		}
		if d["A"] != nil {
			value, e := renameAttributeIDs(s, d["A"], prefix)
			if e != nil {
				return e
			}
			d["A"] = value
		}
		if id := d["ID"]; id != nil {
			text, e := structureID(s, id)
			if e != nil {
				return e
			}
			d["ID"] = destinationLiteral(prefix + text)
		}
		return walk(d["K"], ref, depth+1)
	}
	return walk(parent["K"], parentRef, 0)
}

func rebuildStructureIndexes(c context.Context, s *model.Context, root types.Dict) error {
	// Remove old namespace keys, including those on selected-out objects. New
	// keys are assigned only to owners reached from the retained semantic tree.
	for _, entry := range s.Table {
		if entry != nil && !entry.Free {
			switch d := entry.Object.(type) {
			case types.Dict:
				delete(d, "StructParents")
				delete(d, "StructParent")
			case types.StreamDict:
				delete(d.Dict, "StructParents")
				delete(d.Dict, "StructParent")
			}
		}
	}
	owners := map[int]int{}
	values := []types.Object{}
	ids := map[string]types.Object{}
	var walk func(types.Object, types.Object, types.Object, int) error
	walk = func(o, page, parent types.Object, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 256 {
			return errors.New("structure nesting exceeds 256")
		}
		v, err := s.Dereference(o)
		if err != nil {
			return err
		}
		switch d := v.(type) {
		case nil:
			return nil
		case types.Array:
			for _, x := range d {
				if err = walk(x, page, parent, depth+1); err != nil {
					return err
				}
			}
			return nil
		case types.Integer:
			return errors.New("unresolved integer structure child")
		case types.Dict:
			if d["Pg"] != nil {
				page = d["Pg"]
			}
			if d["S"] != nil {
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
				return walk(d["K"], page, o, depth+1)
			}
			typ, _ := nameValue(s, d["Type"])
			owner := page
			single := false
			index := 0
			switch typ {
			case "MCR":
				if d["Stm"] != nil {
					owner = d["Stm"]
				}
				n, e := s.DereferenceInteger(d["MCID"])
				if e != nil || n == nil || n.Value() < 0 || n.Value() > 1_000_000 {
					return errors.New("invalid structure MCID")
				}
				index = n.Value()
			case "OBJR":
				owner = d["Obj"]
				single = true
			default:
				return errors.New("unsupported structure reference")
			}
			ref, ok := owner.(types.IndirectRef)
			if !ok {
				return errors.New("structure owner is not indirect")
			}
			value, e := s.Dereference(ref)
			if e != nil {
				return e
			}
			var dict types.Dict
			switch x := value.(type) {
			case types.Dict:
				dict = x
			case types.StreamDict:
				dict = x.Dict
			default:
				return errors.New("invalid structure owner")
			}
			key, found := owners[ref.ObjectNumber.Value()]
			if !found {
				key = len(values)
				owners[ref.ObjectNumber.Value()] = key
				values = append(values, nil)
			}
			if single {
				if values[key] != nil {
					return errors.New("annotation has multiple structure owners")
				}
				dict["StructParent"] = types.Integer(key)
				values[key] = parent
			} else {
				dict["StructParents"] = types.Integer(key)
				a, _ := values[key].(types.Array)
				for len(a) <= index {
					a = append(a, nil)
				}
				if a[index] != nil {
					return errors.New("MCID has multiple structure owners")
				}
				a[index] = parent
				values[key] = a
			}
			return nil
		default:
			return fmt.Errorf("unsupported structure child %T", v)
		}
	}
	if err := walk(root["K"], nil, nil, 0); err != nil {
		return err
	}
	nums := types.Array{}
	for i, v := range values {
		nums = append(nums, types.Integer(i), v)
	}
	tree, err := s.IndRefForNewObject(types.Dict{"Nums": nums})
	if err != nil {
		return err
	}
	root["ParentTree"] = *tree
	root["ParentTreeNextKey"] = types.Integer(len(values))
	if len(ids) > 0 {
		keys := make([]string, 0, len(ids))
		for k := range ids {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		names := types.Array{}
		for _, k := range keys {
			names = append(names, destinationLiteral(k), ids[k])
		}
		ref, e := s.IndRefForNewObject(types.Dict{"Names": names})
		if e != nil {
			return e
		}
		root["IDTree"] = *ref
	}
	return nil
}

func discardStructure(c context.Context, s *model.Context) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	delete(cat, "StructTreeRoot")
	delete(cat, "MarkInfo")
	return walkObjects(c, s, func(d types.Dict) error { delete(d, "StructParents"); delete(d, "StructParent"); return nil })
}

// structureElements visits retained structure elements without following parent
// links, namespace maps or content references back into the tree.
func structureElements(c context.Context, s *model.Context, o types.Object, page types.Object, visit func(types.Dict, types.Object) error, depth int) error {
	if err := c.Err(); err != nil {
		return err
	}
	if depth > 256 {
		return errors.New("structure nesting exceeds 256")
	}
	v, err := s.Dereference(o)
	if err != nil {
		return err
	}
	if a, ok := v.(types.Array); ok {
		for _, x := range a {
			if err = structureElements(c, s, x, page, visit, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	d, ok := v.(types.Dict)
	if !ok || d["S"] == nil {
		return nil
	}
	if d["Pg"] != nil {
		page = d["Pg"]
	}
	if err = visit(d, page); err != nil {
		return err
	}
	return structureElements(c, s, d["K"], page, visit, depth+1)
}

func pruneStructurePages(c context.Context, s *model.Context, root types.Dict, kept map[int]bool) error {
	return structureElements(c, s, root["K"], nil, func(d types.Dict, _ types.Object) error {
		if ref, ok := d["Pg"].(types.IndirectRef); ok && !kept[ref.ObjectNumber.Value()] {
			delete(d, "Pg")
		}
		// Ref is an optional cross-reference to other structure elements. Retain only
		// targets with selected semantic descendants; never resurrect omitted pages.
		refs, err := s.DereferenceArray(d["Ref"])
		if err != nil {
			return err
		}
		if refs != nil {
			out := types.Array{}
			for _, ref := range refs {
				target, e := s.DereferenceDict(ref)
				if e != nil {
					return e
				}
				if target != nil && target["K"] != nil {
					out = append(out, ref)
				}
			}
			if len(out) == 0 {
				delete(d, "Ref")
			} else {
				d["Ref"] = out
			}
		}
		return nil
	}, 0)
}

func fitStructureAttributes(c context.Context, s *model.Context, root types.Dict, page types.IndirectRef, m matrix.Matrix) error {
	classes, err := s.DereferenceDict(root["ClassMap"])
	if err != nil {
		return err
	}
	classes = clonedDirectDict(classes)
	if len(classes) > 0 {
		root["ClassMap"] = classes
	}
	classNames := map[string]string{}
	return structureElements(c, s, root["K"], nil, func(d types.Dict, pg types.Object) error {
		if pg == nil {
			for _, attributes := range []types.Object{d["A"], d["C"]} {
				found, e := hasStructureBBox(s, attributes, classes, 0)
				if e != nil {
					return e
				}
				if found {
					return refuse(diag.SourceUnsupported, "A structure bounding box has no page scope; fitting cannot preserve its geometry. Use original page sizes or a reviewed source with explicit page association.")
				}
			}
		}
		if !sameRef(pg, page) {
			return nil
		}
		var transform func(types.Object) (types.Object, error)
		transform = func(o types.Object) (types.Object, error) {
			v, err := s.Dereference(o)
			if err != nil {
				return nil, err
			}
			if a, ok := v.(types.Array); ok {
				out := make(types.Array, len(a))
				for i, x := range a {
					out[i], err = transform(x)
					if err != nil {
						return nil, err
					}
				}
				return out, nil
			}
			attrs, ok := v.(types.Dict)
			if !ok {
				return o, nil
			}
			attrs = clonedDirectDict(attrs)
			if attrs["BBox"] != nil {
				a, e := s.DereferenceArray(attrs["BBox"])
				if e != nil || len(a) != 4 {
					return nil, errors.New("invalid structure BBox")
				}
				coords := make([]float64, 4)
				for i, x := range a {
					coords[i], e = s.DereferenceNumber(x)
					if e != nil {
						return nil, e
					}
				}
				box := types.Rectangle{LL: types.Point{X: coords[0], Y: coords[1]}, UR: types.Point{X: coords[2], Y: coords[3]}}
				attrs["BBox"] = fitRectangle(box, m).Array()
			}
			return attrs, nil
		}
		if d["A"] != nil {
			value, err := transform(d["A"])
			if err != nil {
				return err
			}
			d["A"] = value
		}
		if d["C"] != nil {
			value, e := mapAttributeClasses(s, d["C"], func(value types.Name) (types.Object, error) {
				original := string(value)
				name, found := classNames[original]
				if !found {
					if classes[original] == nil {
						return nil, errors.New("structure class is absent from ClassMap")
					}
					attribute, e := transform(classes[original])
					if e != nil {
						return nil, e
					}
					name = fmt.Sprintf("%s_FitPage%d", original, page.ObjectNumber.Value())
					for suffix := 1; classes[name] != nil; suffix++ {
						name = fmt.Sprintf("%s_FitPage%d_%d", original, page.ObjectNumber.Value(), suffix)
					}
					classes[name] = attribute
					classNames[original] = name
				}
				return types.Name(name), nil
			}, 0)
			if e != nil {
				return e
			}
			d["C"] = value
		}
		return nil
	}, 0)
}

func sameRef(a, b types.Object) bool {
	left, ok := a.(types.IndirectRef)
	right, other := b.(types.IndirectRef)
	return ok && other && left == right
}

// Attribute dictionaries can refer to table header IDs. Renaming the IDTree
// without these consumers would silently break table semantics after a merge.
func renameAttributeIDs(s *model.Context, o types.Object, prefix string) (types.Object, error) {
	return mapStructureAttributes(s, o, func(d types.Dict) error {
		ids, err := tableHeaderIDs(s, d)
		if err != nil {
			return err
		}
		if ids != nil {
			out := make(types.Array, len(ids))
			for i, id := range ids {
				out[i] = destinationLiteral(prefix + id)
			}
			d["Headers"] = out
		}
		return nil
	}, 0)
}

func hasStructureBBox(s *model.Context, o types.Object, classes types.Dict, depth int) (bool, error) {
	if depth > 256 {
		return false, errors.New("structure attribute nesting exceeds 256")
	}
	value, err := s.Dereference(o)
	if err != nil {
		return false, err
	}
	switch v := value.(type) {
	case types.Dict:
		return v["BBox"] != nil, nil
	case types.Array:
		for _, child := range v {
			found, e := hasStructureBBox(s, child, classes, depth+1)
			if e != nil || found {
				return found, e
			}
		}
	case types.Name:
		if classes[string(v)] != nil {
			return hasStructureBBox(s, classes[string(v)], classes, depth+1)
		}
	}
	return false, nil
}
