// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type destination struct {
	page   int
	tail   types.Array
	legacy bool
}

func readDestinations(c context.Context, s *model.Context) (map[string]destination, error) {
	pages, err := rawPages(c, s)
	if err != nil {
		return nil, err
	}
	index := map[types.IndirectRef]int{}
	for i, page := range pages {
		index[page.ref] = i + 1
	}
	result := map[string]destination{}
	decode := func(o types.Object, legacy bool) (destination, error) {
		o, err := s.Dereference(o)
		if err != nil {
			return destination{}, err
		}
		if d, ok := o.(types.Dict); ok {
			o, err = s.Dereference(d["D"])
			if err != nil {
				return destination{}, err
			}
		}
		a, ok := o.(types.Array)
		dest := destination{legacy: legacy}
		if ok && len(a) > 0 {
			if ref, ok := a[0].(types.IndirectRef); ok {
				dest.page = index[ref]
			}
			for _, entry := range a[1:] {
				value, err := s.Dereference(entry)
				if err != nil {
					return destination{}, err
				}
				switch value.(type) {
				case nil, types.Name, types.Integer, types.Float:
					dest.tail = append(dest.tail, value)
				default:
					return destination{}, refuse("source_invalid", "Unsupported destination coordinate value.")
				}
			}
		}
		return dest, nil
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	names, err := s.DereferenceDict(cat["Names"])
	if err != nil {
		return nil, err
	}
	seen := map[types.IndirectRef]bool{}
	var walk func(types.Object, int) error
	walk = func(o types.Object, depth int) error {
		if err := c.Err(); err != nil {
			return err
		}
		if depth > 64 {
			return refuse("source_invalid", "Destination name tree exceeds depth 64.")
		}
		if o == nil {
			return nil
		}
		if ref, ok := o.(types.IndirectRef); ok {
			if seen[ref] {
				return refuse("source_invalid", "Destination name tree repeats a node.")
			}
			seen[ref] = true
		}
		d, err := s.DereferenceDict(o)
		if err != nil {
			return err
		}
		a, err := s.DereferenceArray(d["Names"])
		if err != nil {
			return err
		}
		if len(a)%2 != 0 {
			return refuse("source_invalid", "Odd destination name array.")
		}
		for i := 0; i < len(a); i += 2 {
			name, err := destinationName(s, a[i])
			if err != nil {
				return err
			}
			dest, e := decode(a[i+1], false)
			if e != nil {
				return e
			}
			if _, exists := result[name]; exists {
				return refuse("source_invalid", "Duplicate destination byte-string keys are unsupported.")
			}
			result[name] = dest
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
	if err = walk(names["Dests"], 0); err != nil {
		return nil, err
	}
	legacy, err := s.DereferenceDict(cat["Dests"])
	if err != nil {
		return nil, err
	}
	for name, o := range legacy {
		dest, e := decode(o, true)
		if e != nil {
			return nil, e
		}
		if existing, ok := result[name]; ok && (existing.page != dest.page || !reflect.DeepEqual(existing.tail, dest.tail)) {
			return nil, refuse("source_invalid", "The same destination byte name has conflicting name-tree and legacy targets; this combination is unsupported.")
		}
		result[name] = dest
	}
	return result, nil
}

func detachDestinations(c context.Context, s *model.Context, used map[string]bool, first int64) (map[string]destination, bool, error) {
	originals, err := readDestinations(c, s)
	if err != nil {
		return nil, false, err
	}
	names := make([]string, 0, len(originals))
	for name := range originals {
		names = append(names, name)
	}
	slices.Sort(names)
	replacements := map[string]string{}
	result := map[string]destination{}
	renamed := false
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
			renamed = true
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
		return nil, false, err
	}
	pageSet := map[types.IndirectRef]bool{}
	for _, page := range pages {
		pageSet[page.ref] = true
	}
	err = walkRetained(c, s, func(types.Dict) error { return nil }, func(d types.Dict, key string) (bool, error) {
		keep, err := localTarget(s, d[key], targetNames, pageSet)
		if err != nil || !keep {
			return keep, err
		}
		value, err := s.Dereference(d[key])
		if err != nil {
			return false, err
		}
		if _, array := value.(types.Array); array || value == nil {
			return true, nil
		}
		name, err := destinationName(s, value)
		if err != nil {
			return false, err
		}
		if replacement, ok := replacements[name]; ok {
			if _, isName := value.(types.Name); isName {
				d[key] = types.Name(replacement)
			} else {
				d[key] = destinationLiteral(replacement)
			}
		}
		return true, nil
	}, true)
	if err != nil {
		return nil, false, err
	}
	cat, err := s.Catalog()
	if err != nil {
		return nil, false, err
	}
	delete(cat, "Dests")
	nameDict, err := s.DereferenceDict(cat["Names"])
	if err != nil {
		return nil, false, err
	}
	delete(nameDict, "Dests")
	delete(s.Names, "Dests")
	// No other document-level name tree is part of the output policy.
	delete(cat, "Names")
	clear(s.Names)
	return result, renamed, nil
}

func destinationName(s *model.Context, o types.Object) (string, error) {
	value, err := s.Dereference(o)
	if err != nil {
		return "", err
	}
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
