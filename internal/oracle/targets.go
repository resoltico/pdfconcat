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
	for _, ref := range d.annotationsOf(p) {
		a := d.dict(ref)
		if dest, ok := a["/Dest"]; ok {
			target, err := d.target(dest)
			if err != nil {
				return nil, err
			}
			result = append(result, target)
		}
		targets, err := d.actionTargets(a["/A"], 0)
		if err != nil {
			return nil, err
		}
		result = append(result, targets...)
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
