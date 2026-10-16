// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package oracle

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

func (d *Document) target(dest any) (int, error) {
	index := map[string]int{}
	for i, ref := range d.pages {
		index[ref] = i + 1
	}
	var result destinationPage
	if name, ok := d.deref(dest).(string); ok {
		result = d.resolveName(name, d.namedDestinations(), index)
	} else {
		result = d.destinationPage(dest, index)
	}
	if result.kind != "ok" {
		return 0, fmt.Errorf("destination is %s", result.kind)
	}
	return result.index, nil
}

func (d *Document) actionTargets(action any, depth int) ([]int, error) {
	if depth > maxOutlineDepth {
		return nil, errors.New("action chain exceeds oracle depth")
	}
	if list, ok := d.deref(action).([]any); ok {
		var result []int
		for _, a := range list {
			targets, err := d.actionTargets(a, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, targets...)
		}
		return result, nil
	}
	a := d.dict(action)
	var result []int
	if d.deref(a["/S"]) == "/GoTo" {
		target, err := d.target(a["/D"])
		if err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	if next := a["/Next"]; next != nil {
		targets, err := d.actionTargets(next, depth+1)
		if err != nil {
			return nil, err
		}
		result = append(result, targets...)
	}
	return result, nil
}

// OutlineTargets returns destinations in outline order, including GoTo/Next actions.
func (d *Document) OutlineTargets() ([]int, error) {
	var walk func(any, int) ([]int, error)
	walk = func(node any, depth int) ([]int, error) {
		if depth > maxOutlineDepth {
			return nil, errors.New("outline exceeds oracle depth")
		}
		var result []int
		for count := 0; node != nil; count++ {
			if count > len(d.objects) {
				return nil, errors.New("outline sibling cycle")
			}
			item := d.dict(node)
			if dest, ok := item["/Dest"]; ok {
				target, err := d.target(dest)
				if err != nil {
					return nil, err
				}
				result = append(result, target)
			}
			if action := item["/A"]; action != nil {
				targets, err := d.actionTargets(action, 0)
				if err != nil {
					return nil, err
				}
				result = append(result, targets...)
			}
			if child := item["/First"]; child != nil {
				targets, err := walk(child, depth+1)
				if err != nil {
					return nil, err
				}
				result = append(result, targets...)
			}
			node = item["/Next"]
		}
		return result, nil
	}
	return walk(d.dict(d.catalog()["/Outlines"])["/First"], 0)
}

// ActionTargets returns GoTo targets in annotations and page additional actions.
func (d *Document) ActionTargets(page int) ([]int, error) {
	if page < 1 || page > len(d.pages) {
		return nil, errors.New("page out of range")
	}
	p := d.dict(d.pages[page-1])
	var result []int
	pending := d.annotationsOf(p)
	seen := map[string]bool{}
	for len(pending) > 0 {
		ref := pending[0]
		pending = pending[1:]
		if key, ok := ref.(string); ok {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		a := d.dict(ref)
		for _, key := range []string{"/Popup", "/IRT"} {
			if a[key] != nil {
				pending = append(pending, a[key])
			}
		}
		if d.deref(a["/Subtype"]) == "/Popup" && a["/Parent"] != nil {
			pending = append(pending, a["/Parent"])
		}

		subtype := d.deref(a["/Subtype"])
		if dest, ok := a["/Dest"]; ok && subtype == "/Link" {
			target, err := d.target(dest)
			if err != nil {
				return nil, err
			}
			result = append(result, target)
		}
		var primary any
		if subtype == "/Link" || subtype == "/Widget" || subtype == "/Screen" {
			primary = a["/A"]
		}
		targets, err := d.actionTargets(primary, 0)
		if err != nil {
			return nil, err
		}
		result = append(result, targets...)
		for _, action := range d.dict(a["/AA"]) {
			targets, err := d.actionTargets(action, 0)
			if err != nil {
				return nil, err
			}
			result = append(result, targets...)
		}
	}
	for _, action := range d.dict(p["/AA"]) {
		targets, err := d.actionTargets(action, 0)
		if err != nil {
			return nil, err
		}
		result = append(result, targets...)
	}
	return result, nil
}

// AnnotationDestination returns the explicit destination's page and mode/operand
// tail for a zero-based annotation index on a one-based page. It resolves PDF
// indirect values through qpdf's object graph independently of the adapter.
func (d *Document) AnnotationDestination(page, annotation int) (int, []any, error) {
	if page < 1 || page > len(d.pages) {
		return 0, nil, errors.New("page out of range")
	}
	annotations := d.annotationsOf(d.dict(d.pages[page-1]))
	if annotation < 0 || annotation >= len(annotations) {
		return 0, nil, errors.New("annotation out of range")
	}
	object := d.dict(annotations[annotation])["/Dest"]
	destination := d.list(object)
	if len(destination) < 2 {
		return 0, nil, errors.New("annotation has no explicit destination")
	}
	target, err := d.target(object)
	if err != nil {
		return 0, nil, err
	}
	tail := make([]any, 0, len(destination)-1)
	for _, operand := range destination[1:] {
		tail = append(tail, d.deref(operand))
	}
	return target, tail, nil
}

// FormDefaultFontNames returns the final AcroForm DR Font resource keys.
func (d *Document) FormDefaultFontNames() []string {
	fonts := d.dict(d.dict(d.dict(d.catalog()["/AcroForm"])["/DR"])["/Font"])
	names := make([]string, 0, len(fonts))
	for name := range fonts {
		names = append(names, strings.TrimPrefix(name, "/"))
	}
	slices.Sort(names)
	return names
}

// AnnotationRects returns page annotation rectangles without inferring geometry from the PDF engine.
func (d *Document) AnnotationRects(page int) ([][4]float64, error) {
	if page < 1 || page > len(d.pages) {
		return nil, errors.New("page out of range")
	}
	var rects [][4]float64
	for _, annotation := range d.annotationsOf(d.dict(d.pages[page-1])) {
		values := d.list(d.dict(annotation)["/Rect"])
		if len(values) != 4 {
			return nil, errors.New("annotation Rect does not have four coordinates")
		}
		var rect [4]float64
		for i, value := range values {
			n, ok := value.(float64)
			if !ok {
				return nil, errors.New("annotation coordinate is not numeric")
			}
			rect[i] = n
		}
		rects = append(rects, rect)
	}
	return rects, nil
}

// FieldActionTargets includes field-only additional actions, independently of page annotation roots.
func (d *Document) FieldActionTargets() ([]int, error) {
	pending := d.list(d.dict(d.catalog()["/AcroForm"])["/Fields"])
	seen := map[string]bool{}
	var targets []int
	for len(pending) > 0 {
		node := pending[0]
		pending = pending[1:]
		if ref, ok := node.(string); ok {
			if seen[ref] {
				continue
			}
			seen[ref] = true
		}
		field := d.dict(node)
		pending = append(pending, d.list(field["/Kids"])...)
		actions := d.dict(field["/AA"])
		keys := make([]string, 0, len(actions))
		for key := range actions {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			values, err := d.actionTargets(actions[key], 0)
			if err != nil {
				return nil, err
			}
			targets = append(targets, values...)
		}
	}
	return targets, nil
}
