// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type numberEntry struct {
	value    types.Object
	location diag.PDFLocation
}
type structureNormalization struct {
	stale map[int]numberEntry
	ids   map[string]types.Object
}

func readNumberTree(c context.Context, s *model.Context, o types.Object, location diag.PDFLocation) (map[int]numberEntry, error) {
	out := map[int]numberEntry{}
	seen := map[int]bool{}
	var walk func(types.Object, diag.PDFLocation, int) error
	walk = func(o types.Object, loc diag.PDFLocation, depth int) error {
		if e := c.Err(); e != nil {
			return e
		}
		if depth > 256 {
			return structureAt(errors.New("number tree nesting exceeds 256"), loc)
		}
		if ref, ok := o.(types.IndirectRef); ok {
			n := ref.ObjectNumber.Value()
			loc.Object = n
			if seen[n] {
				return structureAt(errors.New("cyclic number tree"), loc)
			}
			seen[n] = true
			if !liveReference(s, ref) {
				return structureAt(errors.New("number tree node is missing"), loc)
			}
		}
		d, e := s.DereferenceDict(o)
		if e != nil {
			return structureAt(e, loc)
		}
		if d == nil {
			return nil
		}
		nums, e := s.DereferenceArray(d["Nums"])
		if e != nil {
			return structureAt(e, loc)
		}
		if len(nums)%2 != 0 {
			return structureAt(errors.New("odd number tree Nums"), loc)
		}
		for i := 0; i < len(nums); i += 2 {
			p := loc
			p.Path += fmt.Sprintf(".Nums[%d]", i)
			key, e := s.DereferenceInteger(nums[i])
			if e != nil || key == nil || key.Value() < 0 {
				return structureAt(errors.New("invalid number tree index"), p)
			}
			p.Path += fmt.Sprintf("(key=%d)", key.Value())
			if _, ok := out[key.Value()]; ok {
				return structureAt(errors.New("duplicate number tree index"), p)
			}
			out[key.Value()] = numberEntry{nums[i+1], p}
		}
		kids, e := s.DereferenceArray(d["Kids"])
		if e != nil {
			return structureAt(e, loc)
		}
		for i, kid := range kids {
			p := loc
			p.Path += fmt.Sprintf(".Kids[%d]", i)
			if e = walk(kid, p, depth+1); e != nil {
				return e
			}
		}
		return nil
	}
	err := walk(o, location, 0)
	return out, err
}

func structureClaims(c context.Context, s *model.Context) (map[int]diag.PDFLocation, error) {
	claims := map[int]diag.PDFLocation{}
	err := objectLocations(c, s, false, func(d types.Dict, loc diag.PDFLocation) error {
		for _, name := range []string{"StructParent", "StructParents"} {
			if d[name] == nil {
				continue
			}
			p := loc
			p.Path += "." + name
			key, e := s.DereferenceInteger(d[name])
			if e != nil || key != nil && key.Value() < 0 {
				return structureAt(errors.New("invalid structure owner parent-tree key"), p)
			}
			if key == nil {
				continue
			}
			if _, ok := claims[key.Value()]; ok {
				return structureAt(errors.New("multiple objects claim the same parent-tree key"), p)
			}
			claims[key.Value()] = p
		}
		return nil
	})
	return claims, err
}

func applyStructureNormalization(c context.Context, s *model.Context, n *structureNormalization) ([]diag.Diagnostic, error) {
	root, e := structureRoot(s)
	if e != nil {
		return nil, e
	}
	var ds []diag.Diagnostic
	if len(n.stale) > 0 {
		cat, e := s.Catalog()
		if e != nil {
			return nil, e
		}
		location := diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog.StructTreeRoot.ParentTree"}
		if ref, ok := cat["StructTreeRoot"].(types.IndirectRef); ok {
			location.Object = ref.ObjectNumber.Value()
			location.Path = "StructTreeRoot.ParentTree"
		}
		entries, e := readNumberTree(c, s, root["ParentTree"], location)
		if e != nil {
			return nil, e
		}
		keys := make([]int, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		nums := types.Array{}
		maxKey := -1
		for _, key := range keys {
			entry := entries[key]
			if _, drop := n.stale[key]; drop {
				d := diag.Report(diag.SourceStructureIndexNormalized, diag.AnUnclaimedStaleParentTreeEntryIsOmittedRetainedSemanticOwnership())
				loc := entry.location
				d.PDFLocation = &loc
				diag.AttachEvidence(&d, diag.StaleStructureIndexEvidence(int64(key), fmt.Sprint(entry.value)))
				ds = append(ds, d)
				continue
			}
			nums = append(nums, types.Integer(key), entry.value)
			maxKey = key
		}
		ref, e := s.IndRefForNewObject(types.Dict{"Nums": nums})
		if e != nil {
			return nil, e
		}
		root["ParentTree"] = *ref
		if maxKey < math.MaxInt {
			root["ParentTreeNextKey"] = types.Integer(maxKey + 1)
		} else {
			delete(root, "ParentTreeNextKey")
		}
	}
	if len(n.ids) > 0 {
		keys := make([]string, 0, len(n.ids))
		for key := range n.ids {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		names := types.Array{}
		for _, key := range keys {
			names = append(names, destinationLiteral(key), n.ids[key])
		}
		ref, e := s.IndRefForNewObject(types.Dict{"Names": names})
		if e != nil {
			return nil, e
		}
		root["IDTree"] = *ref
		cat, e := s.Catalog()
		if e != nil {
			return nil, e
		}
		loc := diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog.StructTreeRoot.IDTree"}
		if r, ok := cat["StructTreeRoot"].(types.IndirectRef); ok {
			loc.Object = r.ObjectNumber.Value()
		}
		d := diag.Report(diag.SourceStructureIndexNormalized, diag.AnAbsentIDTreeIsDerivedFromUniqueValidatedStructureIDs())
		d.PDFLocation = &loc
		diag.AttachEvidence(&d, diag.DerivedStructureIDsEvidence(int64(len(n.ids))))
		ds = append(ds, d)
	}
	return ds, nil
}
