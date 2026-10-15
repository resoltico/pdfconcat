// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"fmt"
	"slices"
	"strings"

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
	add := func(name string, o types.Object, legacy bool) error {
		o, err := s.Dereference(o)
		if err != nil {
			return err
		}
		if d, ok := o.(types.Dict); ok {
			o, err = s.Dereference(d["D"])
			if err != nil {
				return err
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
					return err
				}
				switch value.(type) {
				case nil, types.Name, types.Integer, types.Float:
					dest.tail = append(dest.tail, value)
				default:
					return refuse("source_invalid", "Unsupported destination coordinate value.")
				}
			}
		}
		result[name] = dest
		return nil
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
			name, err := textOf(a[i])
			if err != nil {
				return err
			}
			if err = add(name, a[i+1], false); err != nil {
				return err
			}
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
		// A name-tree destination takes precedence when both structures define it.
		if existing, ok := result[name]; ok {
			existing.legacy = true
			result[name] = existing
			continue
		}
		if err = add(name, o, true); err != nil {
			return nil, err
		}
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
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(utf16Hex(a), utf16Hex(b)) })
	replacements := map[string]string{}
	dropped := map[string]bool{}
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
			dropped[name] = true
			continue
		}
		chosen := name
		if used[chosen] {
			for n := 2; ; n++ {
				chosen = fmt.Sprintf("%s~%d", name, n)
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
	err = walkObjects(c, s, func(d types.Dict) error {
		// Omitted targets must not leave dangling destination/action references.
		for key, value := range d {
			if key == "Dest" {
				resolved, e := s.Dereference(value)
				if e != nil {
					return e
				}
				if name, e := textOf(resolved); e == nil && dropped[name] {
					delete(d, key)
					continue
				}
			}
			resolved, e := s.Dereference(value)
			if e != nil {
				return e
			}
			if action, ok := resolved.(types.Dict); ok && action["S"] == types.Name("GoTo") {
				target, e := s.Dereference(action["D"])
				if e != nil {
					return e
				}
				if name, e := textOf(target); e == nil && dropped[name] {
					delete(d, key)
				}
			}
		}
		rewrite := func(key string) error {
			value, err := s.Dereference(d[key])
			if err != nil {
				return err
			}
			if _, isArray := value.(types.Array); isArray || value == nil {
				return nil
			}
			old, err := textOf(value)
			if err != nil {
				return err
			}
			if replacement, ok := replacements[old]; ok {
				if _, isName := value.(types.Name); isName {
					d[key] = types.Name(replacement)
				} else {
					d[key] = pdfText(replacement)
				}
			}
			return nil
		}
		if d["Dest"] != nil {
			if err := rewrite("Dest"); err != nil {
				return err
			}
		}
		if d["S"] == types.Name("GoTo") && d["D"] != nil {
			return rewrite("D")
		}
		return nil
	})
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

func pdfText(text string) types.HexLiteral { return types.HexLiteral("FEFF" + utf16Hex(text)) }

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
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(utf16Hex(a), utf16Hex(b)) })
	legacy := types.Dict{}
	var tree func([]string) (types.Dict, error)
	tree = func(keys []string) (types.Dict, error) {
		d := types.Dict{"Limits": types.Array{pdfText(keys[0]), pdfText(keys[len(keys)-1])}}
		if len(keys) <= 128 {
			entries := types.Array{}
			for _, name := range keys {
				dest := dests[name]
				if dest.page < 1 || dest.page > len(pages) {
					return nil, fmt.Errorf("output destination page %d out of range", dest.page)
				}
				value := append(types.Array{pages[dest.page-1].ref}, dest.tail...)
				entries = append(entries, pdfText(name), value)
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
