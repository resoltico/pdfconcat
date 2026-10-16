// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package job

import "github.com/resoltico/pdfconcat/internal/diag"

// Declarations derives leaves and preorder section metadata from the declared tree.
// It never predicts padding or unresolved page ranges.
func Declarations(j Job) ([]Leaf, []Section) {
	var leaves []Leaf
	var sections []Section
	var walk func([]Part, string, int)
	walk = func(parts []Part, owner string, depth int) {
		for _, part := range parts {
			if part.Items != nil {
				if part.Title != "" {
					sections = append(sections, Section{Item: part.Pointer, Title: part.Title, Parent: owner, Depth: depth})
					if part.Divider != nil {
						leaves = append(leaves, dividerLeaf(part, part.Pointer))
					}
					walk(part.Items, part.Pointer, depth+1)
				} else {
					walk(part.Items, owner, depth)
				}
				continue
			}
			leaves = append(leaves, declaredLeaf(part, owner))
		}
	}
	walk(j.Parts, "", 0)
	return leaves, sections
}

func declaredLeaf(part Part, owner string) Leaf {
	kind := "source"
	if part.Page != nil {
		kind = "page"
	} else if part.Contents != nil {
		kind = "contents"
	}
	return Leaf{Part: part, Kind: kind, Owner: owner}
}

func dividerLeaf(part Part, owner string) Leaf {
	style := Layer(*part.Divider, PageStyle{Text: TextStyle{Value: part.Title}, Origins: map[string]string{"value": part.Pointer + "/title"}})
	return Leaf{Part: Part{Pointer: part.Pointer + "/divider", Page: &style, Count: 1}, Kind: "divider", Owner: owner}
}

// Resolve computes the final sequence once the independently measured contents
// count is known. Source facts are keyed by the declared occurrence pointer.
// Failure returns no partial physical sequence; declarations remain in Job.
func Resolve(j Job, facts map[string]SourceFacts, contentsCount int) (Layout, []diag.Diagnostic) {
	layout := Layout{PageNumbers: j.PageNumbers}
	var ds []diag.Diagnostic
	leaves, _ := Declarations(j)
	var sourceSize, physicalSize Size
	for _, leaf := range leaves {
		if leaf.Kind == "source" {
			if f, ok := facts[leaf.Pointer]; ok {
				sourceSize = f.FirstSize
				break
			}
		}
	}
	var active []int
	add := func(code diag.Code, message, pointer string) {
		ds = append(ds, diag.New(code, message, pointer))
	}
	canvas := func(size Size, pointer string, padding bool) bool {
		message := "Give an explicit page size when no source is available."
		code := diag.Code("page_size_required")
		switch {
		case size == (Size{}):
		case !(size.Width >= 1) || !(size.Height >= 1):
			code, message = "plan_value", "Inherited generated-page sides must be at least 1 point; give an explicit size."
		case size.Width > MaxSide || size.Height > MaxSide:
			code, message = "page_size_too_large", "Inherited page exceeds 14400 points; give an explicit size."
		default:
			return true
		}
		if padding {
			message = "The preceding physical page cannot supply a supported odd-start blank; fit the packet or omit odd alignment."
		}
		add(code, message, pointer)
		return false
	}
	emit := func(leaf Leaf, padding bool) {
		if layout.TotalPages == MaxPageIndex {
			add("plan_limit", "Packet exceeds the supported page-index range.", leaf.Pointer)
			return
		}
		p := PlacedPart{Leaf: leaf, FirstPage: layout.TotalPages + 1}
		if leaf.Kind == "source" {
			f, ok := facts[leaf.Pointer]
			if !ok || f.Pages < 1 {
				add("source_invalid", "Source page facts are unavailable.", leaf.Pointer)
				return
			}
			p.Pages = f.Pages
			if int64(p.Pages) > MaxPageIndex-layout.TotalPages {
				add("plan_limit", "Packet exceeds the supported page-index range.", leaf.Pointer)
				return
			}
			ranges := f.Selection
			if len(ranges) == 0 {
				ranges = []PageRange{{First: 1, Last: f.Pages}}
			}
			var selected int64
			for _, span := range ranges {
				if span.First < 1 || span.Last < span.First || int64(span.Last-span.First) >= MaxPageIndex-selected {
					add("plan_limit", "Selected source page count exceeds the supported page-index range.", leaf.Pointer)
					return
				}
				p.Mapping = append(p.Mapping, SourceMapping{SourceFirst: span.First, SourceLast: span.Last, OutputFirst: p.FirstPage + selected})
				selected += int64(span.Last-span.First) + 1
			}
			if selected != int64(p.Pages) {
				add("source_invalid", "Selected source ranges and page count disagree.", leaf.Pointer)
				return
			}
			sourceSize, physicalSize = f.LastSize, f.LastSize
		} else {
			p.Pages = leaf.Count
			switch leaf.Kind {
			case "contents":
				p.Pages = contentsCount
				p.Size = leaf.Contents.Size
				if p.Size == (Size{}) {
					p.Size, _ = ParseSize("A4", false)
					if j.FitTo != nil {
						p.Size = *j.FitTo
					}
				}
			case "padding":
				p.Pages, p.Size = 1, physicalSize
				p.Reason, p.BeforeItem = "odd_start", leaf.Pointer
			default:
				p.Size = leaf.Page.Size
				if p.Size == (Size{}) {
					p.Size = sourceSize
					if j.FitTo != nil {
						p.Size = *j.FitTo
					}
				}
			}
			if !canvas(p.Size, leaf.Pointer, padding) {
				return
			}
			if p.Pages < 1 || p.Pages > MaxGeneratedPages || layout.GeneratedPages+int64(p.Pages) > MaxGeneratedPages {
				add("plan_limit", "Generated pages exceed 100000 or the contents page count is unavailable.", leaf.Pointer)
				return
			}
			physicalSize = p.Size
		}
		if int64(p.Pages) > MaxPageIndex-layout.TotalPages {
			add("plan_limit", "Packet exceeds the supported page-index range.", leaf.Pointer)
			return
		}
		if j.FitTo != nil {
			physicalSize = *j.FitTo
		}
		layout.TotalPages += int64(p.Pages)
		p.LastPage = layout.TotalPages
		if leaf.Kind == "source" {
			layout.SourcePages += int64(p.Pages)
		} else {
			layout.GeneratedPages += int64(p.Pages)
		}
		layout.Parts = append(layout.Parts, p)
		if !padding {
			for _, index := range active {
				if layout.Sections[index].Target == 0 {
					layout.Sections[index].Target = p.FirstPage
				}
			}
		}
	}
	var walk func([]Part, string, int)
	walk = func(parts []Part, owner string, depth int) {
		for _, part := range parts {
			if part.Items == nil {
				emit(declaredLeaf(part, owner), false)
				continue
			}
			if part.StartOn == "odd" && layout.TotalPages%2 == 1 {
				emit(Leaf{Part: Part{Pointer: part.Pointer}, Kind: "padding", Owner: owner}, true)
			}
			childOwner, childDepth := owner, depth
			index := -1
			if part.Title != "" {
				index = len(layout.Sections)
				layout.Sections = append(layout.Sections, Section{Item: part.Pointer, Title: part.Title, Parent: owner, Depth: depth, FirstPage: layout.TotalPages + 1})
				active = append(active, index)
				childOwner, childDepth = part.Pointer, depth+1
				if part.Divider != nil {
					emit(dividerLeaf(part, childOwner), false)
				}
			}
			walk(part.Items, childOwner, childDepth)
			if index >= 0 {
				layout.Sections[index].LastPage = layout.TotalPages
				active = active[:len(active)-1]
			}
		}
	}
	walk(j.Parts, "", 0)
	if len(ds) > 0 {
		return Layout{}, ds
	}
	return layout, nil
}
