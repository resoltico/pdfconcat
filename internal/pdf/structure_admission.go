// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type structureIntegrityError struct {
	location diag.PDFLocation
	err      error
}

func (e *structureIntegrityError) Error() string { return e.err.Error() }
func (e *structureIntegrityError) Unwrap() error { return e.err }
func structureAt(err error, location diag.PDFLocation) error {
	if err == nil {
		return nil
	}
	var existing *structureIntegrityError
	if errors.As(err, &existing) {
		return err
	}
	return &structureIntegrityError{location, err}
}

// All original objects remain subject to signature detection, even when their
// semantic graph is about to be detached. Unrelated tag attributes need not
// have name-valued Type/Subtype entries to perform this security inspection.
func originalSignatureChecks(c context.Context, s *model.Context) error {
	return objectLocations(c, s, false, func(d types.Dict, loc diag.PDFLocation) error {
		name := func(o types.Object) (string, error) {
			v, e := s.Dereference(o)
			if e != nil {
				return "", e
			}
			n, ok := v.(types.Name)
			if !ok {
				return "", nil
			}
			return types.DecodeName(string(n))
		}
		signed, signatureField := false, false
		var values []types.Object
		keys := make([]string, 0, len(d))
		for key := range d {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			decoded, e := types.DecodeName(key)
			if e != nil {
				decoded = key
			}
			switch decoded {
			case "Type", "FT":
				value, e := name(d[key])
				if e != nil {
					return e
				}
				if decoded == "Type" && (value == "Sig" || value == "DocTimeStamp") {
					signed = true
				}
				if decoded == "FT" && value == "Sig" {
					signatureField = true
				}
			case "V":
				values = append(values, d[key])
			}
		}
		if signatureField {
			for _, value := range values {
				resolved, e := s.Dereference(value)
				if e != nil {
					return e
				}
				if resolved != nil {
					signed = true
				}
			}
		}
		if signed {
			err := refuse(diag.SourceSigned, diag.TheSourceDeclaresASignatureOrTimestampWhoseAssuranceComposition())
			return structureAt(err, loc)
		}
		return nil
	})
}

// objectLocations retains enclosing indirect identity and direct dictionary
// paths without following reference cycles. Names/depth are general safeguards;
// callers collecting discarded metadata may omit name-identity checks.
func objectLocations(c context.Context, s *model.Context, names bool, visit func(types.Dict, diag.PDFLocation) error) error {
	var walk func(types.Object, diag.PDFLocation, int) error
	walk = func(o types.Object, loc diag.PDFLocation, depth int) error {
		if depth > 128 {
			return structureAt(refuse(diag.SourceUnsupported, diag.DirectObjectNestingExceeds()), loc)
		}
		switch v := o.(type) {
		case types.Dict:
			if e := visit(v, loc); e != nil {
				return structureAt(e, loc)
			}
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				next := loc
				if utf8.ValidString(key) {
					next.Path += "." + key
				} else {
					next.Path += fmt.Sprintf("[%q]", key)
				}
				if e := walk(v[key], next, depth+1); e != nil {
					return e
				}
			}
		case types.StreamDict:
			return walk(v.Dict, loc, depth+1)
		case types.Array:
			for i, x := range v {
				next := loc
				next.Path += fmt.Sprintf("[%d]", i)
				if e := walk(x, next, depth+1); e != nil {
					return e
				}
			}
		case types.Name:
			if names {
				decoded, e := types.DecodeName(string(v))
				if e == nil && decoded != string(v) {
					return structureAt(refuse(diag.SourceUnsupported, diag.UnsupportedLiteralHashNameIdentityUpstreamMayDecodeItTwice()), loc)
				}
			}
		}
		return nil
	}
	keys := make([]int, 0, len(s.Table))
	for n := range s.Table {
		keys = append(keys, n)
	}
	slices.Sort(keys)
	for _, n := range keys {
		if e := c.Err(); e != nil {
			return e
		}
		entry := s.Table[n]
		if entry == nil || entry.Free {
			continue
		}
		o, e := s.Dereference(types.IndirectRef{ObjectNumber: types.Integer(n), GenerationNumber: types.Integer(generation(entry))})
		if e != nil {
			return e
		}
		if e = walk(o, diag.PDFLocation{Object: n, Path: "Object"}, 0); e != nil {
			return e
		}
	}
	if s.DirectInfoDict != nil {
		return walk(s.DirectInfoDict, diag.PDFLocation{Path: "Trailer.Info"}, 0)
	}
	return nil
}

func graphReferences(c context.Context, s *model.Context, roots ...types.Object) (map[int]bool, error) {
	seen := map[int]bool{}
	pending := append([]types.Object(nil), roots...)
	for len(pending) > 0 {
		if e := c.Err(); e != nil {
			return nil, e
		}
		o := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		switch v := o.(type) {
		case types.IndirectRef:
			n := v.ObjectNumber.Value()
			if seen[n] {
				continue
			}
			seen[n] = true
			if !liveReference(s, v) {
				continue
			}
			value, e := s.Dereference(v)
			if e != nil {
				return nil, e
			}
			pending = append(pending, value)
		case types.Dict:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				pending = append(pending, v[key])
			}
		case types.StreamDict:
			pending = append(pending, v.Dict)
		case types.Array:
			pending = append(pending, v...)
		}
	}
	return seen, nil
}

// detachSourceStructure never changes streams or deletes shared artwork/form/
// annotation objects. Only tag-only objects unreachable from retained roots are
// freed, after original signature checks have inspected every live object.
func detachSourceStructure(c context.Context, s *model.Context) ([]diag.PDFLocation, error) {
	cat, e := s.Catalog()
	if e != nil {
		return nil, e
	}
	semantic, e := graphReferences(c, s, cat["StructTreeRoot"], cat["MarkInfo"])
	if e != nil {
		return nil, e
	}
	var locations []diag.PDFLocation
	for _, key := range []string{"StructTreeRoot", "MarkInfo"} {
		if cat[key] != nil {
			locations = append(locations, diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog." + key})
		}
		delete(cat, key)
	}
	s.Tagged = false
	e = objectLocations(c, s, false, func(d types.Dict, loc diag.PDFLocation) error {
		for _, key := range []string{"StructParent", "StructParents"} {
			if value, e := s.Dereference(d[key]); e != nil || value != nil {
				p := loc
				p.Path += "." + key
				locations = append(locations, p)
			}
			delete(d, key)
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	roots := []types.Object{*s.Root}
	if s.Info != nil {
		roots = append(roots, *s.Info)
	} else if s.DirectInfoDict != nil {
		roots = append(roots, s.DirectInfoDict)
	}
	retained, e := graphReferences(c, s, roots...)
	if e != nil {
		return nil, e
	}
	keys := make([]int, 0, len(semantic))
	for n := range semantic {
		keys = append(keys, n)
	}
	slices.Sort(keys)
	for _, n := range keys {
		if !retained[n] {
			if entry := s.Table[n]; entry != nil && !entry.Free {
				if e := s.FreeObject(n); e != nil {
					return nil, e
				}
			}
		}
	}
	return locations, nil
}

func admitSourceStructure(c context.Context, s *model.Context, policy packet.StructurePolicy) ([]diag.Diagnostic, error) {
	cat, e := s.Catalog()
	if e != nil {
		return nil, e
	}
	if policy == packet.StructureDiscard {
		locations, e := detachSourceStructure(c, s)
		if e != nil {
			return nil, e
		}
		if len(locations) == 0 {
			return nil, nil
		}
		d := diag.Report(diag.SourceStructureRemoved, diag.SourceStructureClaimsAndOwnershipAreDiscardedByExplicitPolicy())
		d.PDFLocation = &locations[0]
		diag.AttachEvidence(&d, diag.StructureDiscardEvidence(int64(len(locations))))
		return []diag.Diagnostic{d}, nil
	}
	raw := cat["StructTreeRoot"]
	if ref, ok := raw.(types.IndirectRef); ok && !liveReference(s, ref) {
		return nil, structureAt(errors.New("declared structure root is missing or has the wrong generation"), diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog.StructTreeRoot"})
	}
	value, e := s.Dereference(raw)
	if e != nil {
		return nil, structureAt(e, diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog.StructTreeRoot"})
	}
	if value == nil {
		// MarkInfo false alone is not a claim of semantic coverage.
		marked, e := s.DereferenceDict(cat["MarkInfo"])
		claimed := e != nil
		if !claimed {
			value, e := s.Dereference(marked["Marked"])
			claimed = e != nil || (value != nil && value != types.Boolean(false))
		}
		locations, e := detachSourceStructure(c, s)
		if e != nil {
			return nil, e
		}
		var witness *diag.PDFLocation
		for i := range locations {
			if locations[i].Path != "Catalog.MarkInfo" && locations[i].Path != "Catalog.StructTreeRoot" {
				witness = &locations[i]
				break
			}
		}
		if !claimed && witness == nil {
			return nil, nil
		}
		if witness == nil {
			witness = &locations[0]
		}
		d := diag.Report(diag.SourceStructureOrphaned, diag.NoStructureHierarchyIsSuppliedOrphanStructureClaimsOwnershipAre())
		d.PDFLocation = witness
		diag.AttachEvidence(&d, diag.OrphanStructureEvidence(int64(len(locations))))
		return []diag.Diagnostic{d}, nil
	}
	root, ok := value.(types.Dict)
	rootLoc := diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog.StructTreeRoot"}
	if ref, ok := raw.(types.IndirectRef); ok {
		rootLoc.Object = ref.ObjectNumber.Value()
	}
	if !ok {
		return nil, structureAt(errors.New("declared structure root must be a dictionary"), rootLoc)
	}
	kind, e := nameValue(s, root["Type"])
	if e != nil || kind != "StructTreeRoot" {
		rootLoc.Path += ".Type"
		return nil, structureAt(errors.New("declared structure root Type must be StructTreeRoot"), rootLoc)
	}
	markLoc := diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: "Catalog.MarkInfo"}
	mark, e := s.DereferenceDict(cat["MarkInfo"])
	if e != nil {
		return nil, structureAt(e, markLoc)
	}
	for _, key := range []string{"Marked", "UserProperties", "Suspects"} {
		if mark[key] != nil {
			if _, e = s.DereferenceBoolean(mark[key], model.V10); e != nil {
				markLoc.Path += "." + key
				return nil, structureAt(e, markLoc)
			}
		}
	}
	if root["ParentTreeNextKey"] != nil {
		if key, e := s.DereferenceInteger(root["ParentTreeNextKey"]); e != nil || key != nil && key.Value() < 0 {
			rootLoc.Path += ".ParentTreeNextKey"
			return nil, structureAt(errors.New("invalid ParentTreeNextKey"), rootLoc)
		}
	}
	if e := validateClassMapShape(c, s, root, rootLoc); e != nil {
		return nil, e
	}
	normal := &structureNormalization{}
	if e = validateStructureGraph(c, s, normal); e != nil {
		return nil, e
	}
	return applyStructureNormalization(c, s, normal)
}

// Attribute operation failures retain the original structure root identity even
// when a selected or fitted view has no complete backend error provenance.
func structureOperationError(s *model.Context, path string, err error) error {
	if err == nil {
		return nil
	}
	loc := diag.PDFLocation{Object: s.Root.ObjectNumber.Value(), Path: path}
	if cat, e := s.Catalog(); e == nil {
		if ref, ok := cat["StructTreeRoot"].(types.IndirectRef); ok {
			loc.Object = ref.ObjectNumber.Value()
		}
	}
	return structureAt(err, loc)
}

// ClassMap values are attribute dictionaries or arrays of dictionaries, not
// arbitrary named metadata. Validate even unused declarations before the
// backend, retaining the exact map/entry provenance and explicit-discard remedy.
func validateClassMapShape(c context.Context, s *model.Context, root types.Dict, location diag.PDFLocation) error {
	location.Path = "StructTreeRoot.ClassMap"
	if ref, ok := root["ClassMap"].(types.IndirectRef); ok {
		location.Object = ref.ObjectNumber.Value()
	}
	classes, e := s.DereferenceDict(root["ClassMap"])
	if e != nil {
		return structureAt(e, location)
	}
	names := make([]string, 0, len(classes))
	for name := range classes {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if e := c.Err(); e != nil {
			return e
		}
		loc := location
		loc.Path += fmt.Sprintf("[%q]", name)
		value, e := s.Dereference(classes[name])
		if e != nil {
			return structureAt(e, loc)
		}
		switch v := value.(type) {
		case types.Dict:
		case types.Array:
			for i, member := range v {
				attribute, e := s.DereferenceDict(member)
				if e != nil || attribute == nil {
					loc.Path += fmt.Sprintf("[%d]", i)
					return structureAt(errors.New("ClassMap array member must be an attribute dictionary"), loc)
				}
			}
		default:
			return structureAt(errors.New("ClassMap value must be an attribute dictionary or an array of attribute dictionaries"), loc)
		}
	}
	return nil
}
