// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/resoltico/pdfconcat/internal/core/diag"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// The page slot stays a full reference. Other slots hold resolved PDF scalars.
type explicitDestination struct {
	page                types.IndirectRef
	mode                types.Name
	operands            types.Array
	hasIndirectOperands bool
}

// An undefined operand reference has the value null under ISO 32000-1.
// This resolution rule applies only to destination modes and operands.
func destinationOperand(s *model.Context, o types.Object) (types.Object, bool, error) {
	_, indirect := o.(types.IndirectRef)
	value, _, err := resolveNavigation(s, o)
	return value, indirect, err
}

func readExplicitDestination(s *model.Context, array types.Array) (explicitDestination, error) {
	var d explicitDestination
	bad := func() error { return refuse(diag.SourceUnsupported, diag.InvalidExplicitDestination()) }
	if len(array) < 2 {
		return d, bad()
	}
	d.page, _ = array[0].(types.IndirectRef)
	mode, indirect, err := destinationOperand(s, array[1])
	if err != nil {
		return d, err
	}
	d.mode, _ = mode.(types.Name)
	d.hasIndirectOperands = indirect
	count := 0
	switch d.mode {
	case "Fit", "FitB":
	case "FitH", "FitV", "FitBH", "FitBV":
		count = 1
	case "XYZ":
		count = 3
	case "FitR":
		count = 4
	default:
		return d, bad()
	}
	if len(array) != count+2 {
		return d, bad()
	}
	for _, raw := range array[2:] {
		value, indirect, err := destinationOperand(s, raw)
		if err != nil {
			return d, err
		}
		switch value.(type) {
		case nil:
			if d.mode == "FitR" {
				return d, bad()
			}
		case types.Integer, types.Float:
		default:
			return d, refuse(diag.SourceUnsupported, diag.UnsupportedDestinationCoordinateValue())
		}
		d.hasIndirectOperands = d.hasIndirectOperands || indirect
		d.operands = append(d.operands, value)
	}
	return d, nil
}

func (d explicitDestination) tail() types.Array {
	return append(types.Array{d.mode}, d.operands...)
}

func (d explicitDestination) array() types.Array {
	return append(types.Array{d.page, d.mode}, d.operands...)
}

type destination struct {
	page   int
	tail   types.Array
	legacy bool
	target types.IndirectRef
	sites  []sourceSite
}

func readDestinations(c context.Context, s *model.Context) (map[string]destination, error) {
	return readDestinationDefinitions(c, s, nil)
}

// With restorations supplied, show resolved operands to backend validation
// without changing arrays shared by other owners. Source validation then
// restores the original name-tree slots before packet projection begins.
func readDestinationDefinitions(c context.Context, s *model.Context, restorations *[]func()) (map[string]destination, error) {
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, err
	}
	index := map[types.IndirectRef]int{}
	for i, page := range pages {
		index[page.ref] = i + 1
	}
	result := map[string]destination{}
	decode := func(o types.Object, legacy bool, site sourceSite, replace func(types.Object)) (destination, error) {
		o, err := navigationValue(s, o, site)
		if err != nil {
			return destination{}, err
		}
		wrapper, _ := o.(types.Dict)
		if wrapper != nil {
			if s.PDF20() && wrapper["SD"] != nil {
				if _, err := absentStructureDestination(s, wrapper["SD"], site.key("SD")); err != nil {
					return destination{}, err
				}
			}
			o, err = navigationValue(s, wrapper["D"], site.key("D"))
			if err != nil {
				return destination{}, err
			}
		}
		dest := destination{legacy: legacy}
		a, ok := o.(types.Array)
		if !ok || len(a) == 0 {
			return dest, site.wrap(refuse(diag.SourceUnsupported, diag.InvalidNavigationBinding()))
		}
		if e := checkExplicitStructureDestination(s, a, site); e != nil {
			return destination{}, e
		}
		dest.target, _ = a[0].(types.IndirectRef)
		dest.page = index[dest.target]
		if dest.page == 0 {
			return dest, nil
		}
		explicit, e := readExplicitDestination(s, a)
		if e != nil {
			return destination{}, site.wrap(e)
		}
		dest.tail = explicit.tail()
		if restorations != nil && explicit.hasIndirectOperands {
			var replacement types.Object = explicit.array()
			if wrapper != nil {
				definition := maps.Clone(wrapper)
				definition["D"] = replacement
				replacement = definition
			}
			replace(replacement)
		}
		return dest, nil
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	base := catalogSite(s)
	names, err := navigationContainer[types.Dict](s, cat["Names"], base.key("Names"))
	if err != nil {
		return nil, err
	}
	namesSite := base.key("Names").anchor(cat["Names"], sourceOrigins{})
	seen := map[types.IndirectRef]bool{}
	var walk func(types.Object, sourceSite, int) error
	walk = func(o types.Object, site sourceSite, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 64 {
			return refuse(diag.SourceUnsupported, diag.DestinationNameTreeExceedsDepth())
		}
		if ref, ok := o.(types.IndirectRef); ok {
			if seen[ref] {
				return refuse(diag.SourceInvalid, diag.DestinationNameTreeRepeatsANode())
			}
			seen[ref] = true
		}
		d, err := navigationContainer[types.Dict](s, o, site)
		if err != nil {
			return err
		}
		if d == nil {
			if depth > 0 {
				return site.wrap(refuse(diag.SourceUnsupported, diag.InvalidNavigationBinding()))
			}
			return nil
		}
		site = site.anchor(o, sourceOrigins{})
		limitsSite := site.key("Limits")
		limits, err := navigationContainer[types.Array](s, d["Limits"], limitsSite)
		if err != nil {
			return err
		}
		for i, bound := range limits {
			if _, err := navigationValue(s, bound, limitsSite.index(i)); err != nil {
				return err
			}
		}
		a, err := navigationContainer[types.Array](s, d["Names"], site.key("Names"))
		if err != nil {
			return err
		}
		if len(a)%2 != 0 {
			return refuse(diag.SourceInvalid, diag.OddDestinationNameArray())
		}
		namesValue := d["Names"]
		arraySite := site.key("Names").anchor(namesValue, sourceOrigins{})
		var resolvedEntries types.Array
		for i := 0; i < len(a); i += 2 {
			keySite := arraySite.index(i)
			key, err := navigationValue(s, a[i], keySite)
			if err != nil {
				return err
			}
			name, err := destinationName(key)
			if err != nil {
				return keySite.wrap(err)
			}
			dest, e := decode(a[i+1], false, arraySite.index(i+1), func(o types.Object) {
				if resolvedEntries == nil {
					resolvedEntries = slices.Clone(a)
				}
				resolvedEntries[i+1] = o
			})
			if e != nil {
				return e
			}
			if _, exists := result[name]; exists {
				return refuse(diag.SourceUnsupported, diag.DuplicateDestinationByteStringKeysAreUnsupported())
			}
			dest.sites = []sourceSite{arraySite.index(i + 1)}
			result[name] = dest
		}
		if resolvedEntries != nil {
			*restorations = append(*restorations, func() { d["Names"] = namesValue })
			d["Names"] = resolvedEntries
		}
		kids, err := navigationContainer[types.Array](s, d["Kids"], site.key("Kids"))
		if err != nil {
			return err
		}
		kidSite := site.key("Kids").anchor(d["Kids"], sourceOrigins{})
		for i, kid := range kids {
			if err = walk(kid, kidSite.index(i), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err = walk(names["Dests"], namesSite.key("Dests"), 0); err != nil {
		return nil, err
	}
	legacy, err := navigationContainer[types.Dict](s, cat["Dests"], base.key("Dests"))
	if err != nil {
		return nil, err
	}
	legacyValue := cat["Dests"]
	legacySite := base.key("Dests").anchor(legacyValue, sourceOrigins{})
	legacyNames := make([]string, 0, len(legacy))
	for name := range legacy {
		legacyNames = append(legacyNames, name)
	}
	slices.Sort(legacyNames)
	var resolvedLegacy types.Dict
	for _, name := range legacyNames {
		o := legacy[name]
		dest, e := decode(o, true, legacySite.name(name), func(o types.Object) {
			if resolvedLegacy == nil {
				resolvedLegacy = maps.Clone(legacy)
			}
			resolvedLegacy[name] = o
		})
		if e != nil {
			return nil, legacySite.name(name).wrap(e)
		}
		if existing, ok := result[name]; ok && (existing.page != dest.page || !reflect.DeepEqual(existing.tail, dest.tail)) {
			return nil, refuse(diag.SourceUnsupported, diag.TheSameDestinationByteNameHasConflictingNameTreeAnd())
		}
		dest.sites = []sourceSite{legacySite.name(name)}
		if existing, ok := result[name]; ok {
			dest.sites = append(existing.sites, dest.sites...)
		}
		result[name] = dest
	}
	if resolvedLegacy != nil {
		*restorations = append(*restorations, func() { cat["Dests"] = legacyValue })
		cat["Dests"] = resolvedLegacy
	}
	return result, nil
}

type destinationRename struct {
	from, to string
	sites    []sourceSite
}

func detachDestinations(c context.Context, s *model.Context, used map[string]bool, first int64, original sourceOrigins) (map[string]destination, []destinationRename, error) {
	originals, err := readDestinations(c, s)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(originals))
	for name := range originals {
		names = append(names, name)
	}
	slices.Sort(names)
	replacements := map[string]string{}
	result := map[string]destination{}
	renamed := []destinationRename{}
	// Reserve every original name before choosing suffixes, avoiding future collisions.
	reserved := map[string]bool{}
	for name := range originals {
		reserved[name] = true
	}
	for _, name := range names {
		d := originals[name]
		if d.page < 1 {
			continue
		}
		chosen := name
		if used[chosen] {
			for n := len(used) + 1; ; n++ {
				chosen = fmt.Sprintf("pdfconcat.destination.%d", n)
				if !used[chosen] && !reserved[chosen] {
					break
				}
			}
			replacements[name] = chosen
			sites := d.sites
			if source, ok := original.destinations[name]; ok {
				sites = source.sites
			}
			renamed = append(renamed, destinationRename{from: name, to: chosen, sites: sites})
		}
		used[chosen] = true
		d.page += int(first) - 1
		result[chosen] = d
	}
	targetNames := make(map[string]destination, len(originals)+len(replacements))
	for name, d := range originals {
		targetNames[name] = d
	}
	for old, chosen := range replacements {
		targetNames[chosen] = originals[old]
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, nil, err
	}
	pageSet := map[types.IndirectRef]bool{}
	for _, page := range pages {
		pageSet[page.ref] = true
	}
	origins, e := sourceOriginsFor(c, s)
	if e != nil {
		return nil, nil, e
	}
	err = walkRetained(c, s, origins, nil, func(binding retainedTarget) (bool, error) {
		keep, err := localTarget(binding.value, targetNames, pageSet)
		if err != nil || !keep {
			return keep, err
		}
		if _, array := binding.value.(types.Array); array || binding.value == nil {
			return true, nil
		}
		name, err := destinationName(binding.value)
		if err != nil {
			return false, err
		}
		if replacement, ok := replacements[name]; ok {
			if _, isName := binding.value.(types.Name); isName {
				binding.owner[binding.key] = types.Name(replacement)
			} else {
				binding.owner[binding.key] = destinationLiteral(replacement)
			}
		}
		return true, nil
	}, nil, true)
	if err != nil {
		return nil, nil, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, nil, err
	}
	delete(cat, "Dests")
	// No other document-level name tree is part of the output policy.
	delete(cat, "Names")
	clear(s.Names)
	return result, renamed, nil
}

func destinationName(value types.Object) (string, error) {
	switch name := value.(type) {
	case types.Name:
		return string(name), nil
	case types.StringLiteral:
		bytes, err := types.Unescape(string(name))
		return string(bytes), err
	case types.HexLiteral:
		bytes, err := name.Bytes()
		return string(bytes), err
	default:
		return "", fmt.Errorf("expected destination byte string or name, got %T", value)
	}
}
func destinationLiteral(raw string) types.HexLiteral {
	return types.HexLiteral(hex.EncodeToString([]byte(raw)))
}

func rebuildDestinations(c context.Context, s *model.Context, dests map[string]destination) error {
	if len(dests) == 0 {
		return nil
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(dests))
	for name := range dests {
		names = append(names, name)
	}
	slices.Sort(names)
	legacy := types.Dict{}
	var tree func([]string) (types.Dict, error)
	tree = func(keys []string) (types.Dict, error) {
		d := types.Dict{"Limits": types.Array{destinationLiteral(keys[0]), destinationLiteral(keys[len(keys)-1])}}
		if len(keys) <= 128 {
			entries := types.Array{}
			for _, name := range keys {
				dest := dests[name]
				if dest.page < 1 || dest.page > len(pages) {
					return nil, fmt.Errorf("output destination page %d out of range", dest.page)
				}
				value := append(types.Array{pages[dest.page-1].ref}, dest.tail...)
				entries = append(entries, destinationLiteral(name), value)
				if dest.legacy {
					legacy[name] = value
				}
			}
			d["Names"] = entries
		} else {
			kids := types.Array{}
			// Split into <=128 subtrees with bounded leaf sizes.
			span := (len(keys) + 127) / 128
			for start := 0; start < len(keys); start += span {
				child, err := tree(keys[start:min(start+span, len(keys))])
				if err != nil {
					return nil, err
				}
				ref, err := s.IndRefForNewObject(child)
				if err != nil {
					return nil, err
				}
				kids = append(kids, *ref)
			}
			d["Kids"] = kids
		}
		return d, nil
	}
	root, err := tree(names)
	if err != nil {
		return err
	}
	ref, err := s.IndRefForNewObject(root)
	if err != nil {
		return err
	}
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	cat["Names"] = types.Dict{"Dests": *ref}
	if len(legacy) > 0 {
		cat["Dests"] = legacy
	}
	clear(s.Names)
	return nil
}

func refuseStructureDestination(site sourceSite) error {
	return site.wrap(refuse(diag.SourceStructureDestination, diag.StructureDestinationUnsupported()))
}

// A structure destination array uses an element reference in the page slot.
// A declaration using PDF 2.0 navigation must not be silently projected away.
func checkExplicitStructureDestination(s *model.Context, array types.Array, site sourceSite) error {
	if !s.PDF20() || len(array) == 0 {
		return nil
	}
	ref, ok := array[0].(types.IndirectRef)
	if !ok || !liveReference(s, ref) {
		return nil
	}
	target, err := s.Dereference(ref)
	if err != nil {
		return err
	}
	dict, ok := target.(types.Dict)
	if !ok {
		return nil
	}
	kind, err := nameValue(s, dict["Type"])
	if err != nil {
		return err
	}
	if kind == "StructElem" || kind == "" && dict["S"] != nil && dict["P"] != nil {
		return refuseStructureDestination(site)
	}
	return nil
}

// A live null carries no structure destination. Check the reference first
// because the backend can also return nil for an undefined object.
func absentStructureDestination(s *model.Context, raw types.Object, site sourceSite) (bool, error) {
	val, defined, err := resolveNavigation(s, raw)
	if !defined {
		return false, refuseStructureDestination(site)
	}
	if err != nil {
		return false, site.wrap(err)
	}
	if val != nil && s.PDF20() {
		return false, refuseStructureDestination(site)
	}
	return val == nil, nil
}
