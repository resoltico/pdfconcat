// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package oracle

import (
	"errors"
	"fmt"
	"reflect"
)

// Outline is the ordered semantic hierarchy asserted from fixture intent.
type Outline struct {
	Title    string
	Targets  []int
	Open     bool
	Children []Outline
}

// OutlineTree independently verifies final parent/sibling/end links and signed
// visible-descendant counts, then returns the ordered title/target hierarchy.
func (d *Document) OutlineTree() ([]Outline, error) {
	root := d.catalog()["/Outlines"]
	if root == nil {
		return nil, nil
	}
	if !isReference(root) {
		return nil, errors.New("outline root is not an indirect reference")
	}
	seen := map[string]bool{}
	var siblings func(any, any, int) ([]Outline, int, error)
	siblings = func(parent, node any, depth int) ([]Outline, int, error) {
		if depth > maxOutlineDepth {
			return nil, 0, errors.New("outline hierarchy exceeds depth")
		}
		var nodes []Outline
		var previous any
		visible := 0
		for node != nil {
			ref, ok := node.(string)
			if !ok || !isReference(ref) || seen[ref] {
				return nil, 0, errors.New("outline has non-reference or repeated node")
			}
			seen[ref] = true
			item := d.dict(node)
			if item["/Parent"] != parent || item["/Prev"] != previous {
				return nil, 0, fmt.Errorf("outline parent/previous mismatch at %s", ref)
			}
			title, ok := d.deref(item["/Title"]).(string)
			if !ok {
				return nil, 0, errors.New("outline title missing")
			}
			if len(title) > 2 && (title[:2] == "u:" || title[:2] == "b:") {
				title = title[2:]
			}
			children, descendants, err := siblings(node, item["/First"], depth+1)
			if err != nil {
				return nil, 0, err
			}
			count := 0.0
			if value := item["/Count"]; value != nil {
				var ok bool
				count, ok = d.deref(value).(float64)
				if !ok {
					return nil, 0, errors.New("outline count is not a number")
				}
			}
			open := count > 0
			expected := float64(descendants)
			if count < 0 {
				expected = -expected
			}
			if count != expected {
				return nil, 0, fmt.Errorf("outline count %v expected ±%d at %s", count, descendants, ref)
			}
			var targets []int
			if dest := item["/Dest"]; dest != nil {
				target, err := d.target(dest)
				if err != nil {
					return nil, 0, err
				}
				targets = append(targets, target)
			}
			actions, err := d.actionTargets(item["/A"], 0)
			if err != nil {
				return nil, 0, err
			}
			targets = append(targets, actions...)
			nodes = append(nodes, Outline{Title: title, Targets: targets, Open: open, Children: children})
			visible++
			if open {
				visible += descendants
			}
			previous = node
			node = item["/Next"]
		}
		if d.dict(parent)["/Last"] != previous {
			return nil, 0, errors.New("outline first/last mismatch")
		}
		return nodes, visible, nil
	}
	nodes, visible, err := siblings(root, d.dict(root)["/First"], 0)
	if err != nil {
		return nil, err
	}
	if count := d.dict(root)["/Count"]; count != nil && d.deref(count) != float64(visible) {
		return nil, fmt.Errorf("outline root count %v expected %d", count, visible)
	}
	return nodes, nil
}

// CheckOutlineTree rejects a wrong expected order, target or hierarchy.
func (d *Document) CheckOutlineTree(expected []Outline) error {
	actual, err := d.OutlineTree()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return fmt.Errorf("outline hierarchy %v expected %v", actual, expected)
	}
	return nil
}
