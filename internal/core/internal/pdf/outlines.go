// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

type nativeOutline struct {
	dict            types.Dict
	ref             types.IndirectRef
	children        []*nativeOutline
	open, hadTarget bool
}

func readOutlineForest(c context.Context, s *model.Context) ([]*nativeOutline, error) {
	cat, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	root, err := s.DereferenceDict(cat["Outlines"])
	if err != nil {
		return nil, err
	}
	return readOutlineList(c, s, root["First"])
}
func readOutlineList(c context.Context, s *model.Context, first types.Object) ([]*nativeOutline, error) {
	seen := map[types.IndirectRef]bool{}
	var read func(types.Object, int) ([]*nativeOutline, error)
	read = func(o types.Object, depth int) ([]*nativeOutline, error) {
		if o == nil {
			return nil, nil
		}
		if err := s.CheckRecursionDepth("composed outline hierarchy", depth); err != nil {
			return nil, err
		}
		result := []*nativeOutline{}
		for o != nil {
			if err := c.Err(); err != nil {
				return nil, err
			}
			ref, ok := o.(types.IndirectRef)
			if !ok || seen[ref] {
				return nil, refuse("source_invalid", "Outline nodes must be distinct indirect objects.")
			}
			seen[ref] = true
			d, err := s.DereferenceDict(ref)
			if err != nil {
				return nil, err
			}
			if len(d) == 0 {
				break
			}
			children, err := read(d["First"], depth+1)
			if err != nil {
				return nil, err
			}
			count, err := s.DereferenceInteger(d["Count"])
			if err != nil {
				return nil, err
			}
			open := count == nil || count.Value() >= 0
			result = append(result, &nativeOutline{dict: d, ref: ref, children: children, open: open, hadTarget: d["Dest"] != nil || d["A"] != nil})
			o = d["Next"]
		}
		return result, nil
	}
	return read(first, 0)
}
func pruneOutlineForest(nodes []*nativeOutline) ([]*nativeOutline, bool) {
	result := []*nativeOutline{}
	removed := false
	for _, node := range nodes {
		children, pruned := pruneOutlineForest(node.children)
		node.children = children
		removed = removed || pruned
		if node.hadTarget && node.dict["Dest"] == nil && node.dict["A"] == nil && len(children) == 0 {
			removed = true
			continue
		}
		result = append(result, node)
	}
	return result, removed
}
func installOutlineForest(c context.Context, s *model.Context, nodes []*nativeOutline) error {
	cat, err := s.Catalog()
	if err != nil {
		return err
	}
	delete(cat, "Outlines")
	s.Outlines = nil
	if len(nodes) == 0 {
		return nil
	}
	root := types.Dict{"Type": types.Name("Outlines")}
	ref, err := s.IndRefForNewObject(root)
	if err != nil {
		return err
	}
	var link func([]*nativeOutline, types.IndirectRef, int) (int, error)
	link = func(list []*nativeOutline, parent types.IndirectRef, depth int) (int, error) {
		if len(list) == 0 {
			return 0, nil
		}
		if err := s.CheckRecursionDepth("composed outline hierarchy", depth); err != nil {
			return 0, err
		}
		visible := 0
		for i, node := range list {
			if err := c.Err(); err != nil {
				return 0, err
			}
			d := node.dict
			for _, key := range []string{"Parent", "Prev", "Next", "First", "Last", "Count"} {
				delete(d, key)
			}
			d["Parent"] = parent
			if i > 0 {
				d["Prev"] = list[i-1].ref
			}
			if i+1 < len(list) {
				d["Next"] = list[i+1].ref
			}
			descendants, err := link(node.children, node.ref, depth+1)
			if err != nil {
				return 0, err
			}
			if len(node.children) > 0 {
				d["First"] = node.children[0].ref
				d["Last"] = node.children[len(node.children)-1].ref
				count := descendants
				if !node.open {
					count = -count
				}
				d["Count"] = types.Integer(count)
			}
			visible++
			if node.open {
				visible += descendants
			}
		}
		return visible, nil
	}
	count, err := link(nodes, *ref, 0)
	if err != nil {
		return err
	}
	root["First"] = nodes[0].ref
	root["Last"] = nodes[len(nodes)-1].ref
	root["Count"] = types.Integer(count)
	cat["Outlines"] = *ref
	s.Outlines = root
	return nil
}
func outlineDepth(c context.Context, s *model.Context) (int, error) {
	nodes, err := readOutlineForest(c, s)
	if err != nil {
		return 0, err
	}
	var depth func([]*nativeOutline) int
	depth = func(list []*nativeOutline) int {
		maximum := 0
		for _, node := range list {
			maximum = max(maximum, 1+depth(node.children))
		}
		return maximum
	}
	return depth(nodes), nil
}

type importedOutlineRoot struct {
	root  types.Dict
	owner string
}

func detachOutlineRoot(s *model.Context, owner string) (importedOutlineRoot, error) {
	cat, err := s.Catalog()
	if err != nil {
		return importedOutlineRoot{}, err
	}
	root, err := s.DereferenceDict(cat["Outlines"])
	if err != nil {
		return importedOutlineRoot{}, err
	}
	delete(cat, "Outlines")
	s.Outlines = nil
	return importedOutlineRoot{root: root, owner: owner}, nil
}

func graftOutlines(c context.Context, s *model.Context, layout packet.Layout, imports []importedOutlineRoot) error {
	if len(layout.Sections) == 0 {
		hasOutlines := false
		for _, imported := range imports {
			hasOutlines = hasOutlines || len(imported.root) > 0
		}
		if !hasOutlines {
			return nil
		}
	}
	pages, err := rawPages(c, s)
	if err != nil {
		return err
	}
	sections := map[string]*nativeOutline{}
	parents := make(map[string]string, len(layout.Sections))
	for _, section := range layout.Sections {
		title, err := types.EscapedUTF16String(section.Title)
		if err != nil {
			return err
		}
		d := types.Dict{"Title": types.StringLiteral(*title), "Dest": types.Array{pages[section.Target-1].ref, types.Name("Fit")}}
		ref, err := s.IndRefForNewObject(d)
		if err != nil {
			return err
		}
		sections[section.Item] = &nativeOutline{dict: d, ref: *ref, open: true}
		parents[section.Item] = section.Parent
	}
	roots := []*nativeOutline{}
	// Event order is the resolved source/section sequence, not a second offset map.
	emitted := map[string]bool{}
	var emitSection func(string)
	emitSection = func(item string) {
		if item == "" || emitted[item] {
			return
		}
		parent, known := parents[item]
		if !known {
			return
		}
		emitSection(parent)
		node := sections[item]
		if parent == "" {
			roots = append(roots, node)
		} else {
			sections[parent].children = append(sections[parent].children, node)
		}
		emitted[item] = true
	}
	sourceIndex := 0
	for _, part := range layout.Parts {
		emitSection(part.Owner)
		if part.Source == "" {
			continue
		}
		imported := imports[sourceIndex]
		sourceIndex++
		nodes, err := readOutlineList(c, s, imported.root["First"])
		if err != nil {
			return err
		}
		if imported.owner == "" {
			roots = append(roots, nodes...)
		} else {
			parent := sections[imported.owner]
			parent.children = append(parent.children, nodes...)
		}
	}
	return installOutlineForest(c, s, roots)
}
