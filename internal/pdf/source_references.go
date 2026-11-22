// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

type referenceRoot struct {
	value types.Object
	site  sourceSite
}

// Walk the reachable graph once per full indirect identity. A replacement
// affects only the edge that contains the undefined reference.
func walkReferences(c context.Context, s *model.Context, origins sourceOrigins, roots []referenceRoot,
	undefined func(types.IndirectRef, sourceSite) (types.IndirectRef, error),
	checkLive func(types.IndirectRef, sourceSite) error) ([]func(), error) {
	pending := append([]referenceRoot(nil), roots...)
	seen := map[types.IndirectRef]bool{}
	var edits []func()
	var walk func(types.Object, sourceSite, int) (types.IndirectRef, error)
	walk = func(value types.Object, site sourceSite, depth int) (types.IndirectRef, error) {
		if err := c.Err(); err != nil {
			return types.IndirectRef{}, err
		}
		if depth > 128 {
			return types.IndirectRef{}, site.wrap(refuse(diag.SourceUnsupported, diag.DirectObjectNestingExceeds()))
		}
		switch v := value.(type) {
		case types.IndirectRef:
			if !liveReference(s, v) {
				return undefined(v, site)
			}
			if !seen[v] {
				if checkLive != nil {
					if err := checkLive(v, site); err != nil {
						return types.IndirectRef{}, err
					}
				}
				seen[v] = true
				pending = append(pending, referenceRoot{value: v, site: site.anchor(v, origins)})
			}
		case types.Dict:
			for _, key := range slices.Sorted(maps.Keys(v)) {
				replacement, err := walk(v[key], site.key(key), depth+1)
				if err != nil {
					return types.IndirectRef{}, err
				}
				if replacement != (types.IndirectRef{}) {
					edits = append(edits, func() { v[key] = replacement })
				}
			}
		case types.Array:
			for i, child := range v {
				replacement, err := walk(child, site.index(i), depth+1)
				if err != nil {
					return types.IndirectRef{}, err
				}
				if replacement != (types.IndirectRef{}) {
					edits = append(edits, func() { v[i] = replacement })
				}
			}
		case types.StreamDict:
			return walk(v.Dict, site, depth+1)
		case types.ObjectStreamDict:
			return walk(v.Dict, site, depth+1)
		case types.XRefStreamDict:
			return walk(v.Dict, site, depth+1)
		}
		return types.IndirectRef{}, nil
	}
	for len(pending) > 0 {
		root := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		value := root.value
		ref, indirect := value.(types.IndirectRef)
		if indirect {
			seen[ref] = true
			var err error
			value, err = s.Dereference(ref)
			if err != nil {
				return nil, root.site.wrap(err)
			}
		}
		replacement, err := walk(value, root.site, 0)
		if err != nil {
			return nil, err
		}
		if indirect && replacement != (types.IndirectRef{}) {
			entry := s.Table[ref.ObjectNumber.Value()]
			edits = append(edits, func() { entry.Object = replacement })
		}
	}
	return edits, nil
}

func originalReferenceRoots(s *model.Context) []referenceRoot {
	roots := []referenceRoot{{*s.Root, catalogSite(s)}}
	if s.DirectInfoDict != nil {
		roots = append(roots, referenceRoot{s.DirectInfoDict, sourceSite{path: "Trailer.Info"}})
	}
	return roots
}

// Walk the effective serialized trailer before semantic validation. It
// provides the catalog, metadata and extension roots without following
// unused objects from the xref table.
func checkReadReferences(c context.Context, s *model.Context, rejectUndefined bool) error {
	if s.OriginalTrailer == nil {
		return errors.New("missing effective output trailer")
	}
	roots := []referenceRoot{{s.OriginalTrailer, sourceSite{path: "Trailer"}}}
	_, err := walkReferences(c, s, sourceOrigins{}, roots,
		func(ref types.IndirectRef, site sourceSite) (types.IndirectRef, error) {
			if rejectUndefined {
				return types.IndirectRef{}, site.wrap(fmt.Errorf("dangling output object %s", ref.PDFString()))
			}
			return types.IndirectRef{}, nil
		}, func(ref types.IndirectRef, site sourceSite) error {
			if actual, mismatched := s.Read.MismatchedObjectHeaders[ref]; mismatched {
				return site.wrap(&pdfcpu.ObjectIdentityError{Expected: ref, Actual: actual})
			}
			return nil
		})
	return err
}

// Keep undefined source edges unbound before the first allocation. Object zero
// is permanently free. Negative generations distinguish these internal edges
// from real PDF identities and recover the original reference for diagnostics.
func isolateUndefinedSourceReferences(c context.Context, s *model.Context) (map[types.IndirectRef]types.IndirectRef, error) {
	var originals map[types.IndirectRef]types.IndirectRef
	edits, err := walkReferences(c, s, sourceOrigins{}, originalReferenceRoots(s),
		func(ref types.IndirectRef, _ sourceSite) (types.IndirectRef, error) {
			if originals == nil {
				originals = map[types.IndirectRef]types.IndirectRef{}
			}
			marker := types.IndirectRef{GenerationNumber: -types.Integer(len(originals)) - 1}
			originals[marker] = ref
			return marker, nil
		}, nil)
	if err != nil || len(edits) == 0 {
		return nil, err
	}
	// Check named-destination bindings at their original references, just as
	// the retained Link/GoTo/outline pass does before isolation.
	if _, err := readDestinations(c, s); err != nil {
		return nil, err
	}
	for _, edit := range edits {
		edit()
	}
	return originals, nil
}

func checkRetainedSourceReferences(c context.Context, s *model.Context, origins sourceOrigins) error {
	if len(origins.undefinedReferences) == 0 {
		return nil
	}
	root, err := structureRoot(s)
	if err != nil {
		return err
	}
	if root != nil {
		// Rehearse the real structure contribution on an isolated view. Old
		// parent links and indexes can still reach pages omitted by selection.
		view, err := selectionView(c, s)
		if err != nil {
			return err
		}
		root, err = structureRoot(view)
		if err != nil {
			return err
		}
		part, err := captureStructurePart(view, root)
		if err != nil {
			return err
		}
		if err := combineStructure(c, view, []structurePart{part}); err != nil {
			return err
		}
		s = view
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	var roots []referenceRoot
	base := catalogSite(s)
	for _, key := range slices.Sorted(maps.Keys(retainedSourceCatalogKeys)) {
		raw := cat[key]
		site := base.key(key)
		switch key {
		case "Names":
			names, err := s.DereferenceDict(raw)
			if err != nil {
				return err
			}
			// Other source name trees are removed during catalog cleanup.
			roots = append(roots, referenceRoot{types.Dict{"Dests": names["Dests"]}, site.anchor(raw, origins)})
		case "AcroForm":
			form, err := s.DereferenceDict(raw)
			if err != nil {
				return err
			}
			view := maps.Clone(form)
			delete(view, "XFA")
			roots = append(roots, referenceRoot{view, site.anchor(raw, origins)})
		default:
			roots = append(roots, referenceRoot{types.Dict{key: raw}, base})
		}
	}
	if root != nil {
		roots = append(roots, referenceRoot{types.Dict{"StructTreeRoot": cat["StructTreeRoot"]}, base})
	}
	_, err = walkReferences(c, s, origins, roots,
		func(ref types.IndirectRef, site sourceSite) (types.IndirectRef, error) {
			original, found := origins.undefinedReferences[ref]
			if !found {
				original = ref
			}
			return types.IndirectRef{}, site.wrap(refuse(diag.SourceInvalid, diag.UndefinedRetainedSourceReference(
				int64(original.ObjectNumber.Value()), int64(original.GenerationNumber.Value()))))
		}, nil)
	return err
}
