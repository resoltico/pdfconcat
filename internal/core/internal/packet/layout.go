// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import "github.com/resoltico/pdfconcat/internal/core/internal/diag"

// Resolve maps the cached declaration traversal to physical spans.
func Resolve(j Plan, facts []SourceFacts, contentsCount int) (Layout, []diag.Diagnostic) {
	layout := Layout{PageNumbers: j.PageNumbers}
	source := func(leaf Leaf) (SourceFacts, bool) {
		if leaf.Index < 0 || leaf.Index >= len(facts) {
			return SourceFacts{}, false
		}
		return facts[leaf.Index], true
	}
	var ds []diag.Diagnostic
	leaves := j.Leaves()
	var sourceSize, physicalSize Size
	for _, leaf := range leaves {
		if leaf.Kind == PartSource {
			if f, ok := source(leaf); ok {
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
		code := diag.PageSizeRequired
		switch {
		case size == (Size{}):
		case !(size.Width >= 1) || !(size.Height >= 1):
			code, message = diag.PlanValue, "Inherited generated-page sides must be at least 1 point; give an explicit size."
		case size.Width > MaxSide || size.Height > MaxSide:
			code, message = diag.PageSizeTooLarge, "Inherited page exceeds 14400 points; give an explicit size."
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
			add(diag.PlanLimit, "Packet exceeds the supported page-index range.", leaf.Ref)
			return
		}
		p := Part{Leaf: leaf, FirstPage: layout.TotalPages + 1}
		if leaf.Kind == PartSource {
			f, ok := source(leaf)
			if !ok || f.Pages < 1 {
				add(diag.SourceInvalid, "Source page facts are unavailable.", leaf.Ref)
				return
			}
			p.Pages = f.Pages
			if int64(p.Pages) > MaxPageIndex-layout.TotalPages {
				add(diag.PlanLimit, "Packet exceeds the supported page-index range.", leaf.Ref)
				return
			}
			ranges := f.Selection
			if len(ranges) == 0 {
				ranges = []PageRange{{First: 1, Last: f.Pages}}
			}
			var selected int64
			for _, span := range ranges {
				if span.First < 1 || span.Last < span.First || int64(span.Last-span.First) >= MaxPageIndex-selected {
					add(diag.PlanLimit, "Selected source page count exceeds the supported page-index range.", leaf.Ref)
					return
				}
				p.Mapping = append(p.Mapping, SourceMapping{SourceFirst: span.First, SourceLast: span.Last, OutputFirst: p.FirstPage + selected})
				selected += int64(span.Last-span.First) + 1
			}
			if selected != int64(p.Pages) {
				add(diag.SourceInvalid, "Selected source ranges and page count disagree.", leaf.Ref)
				return
			}
			sourceSize, physicalSize = f.LastSize, f.LastSize
		} else {
			p.Pages = leaf.Count
			switch leaf.Kind {
			case PartContents:
				p.Pages = contentsCount
				p.Size = leaf.Contents.Size
				if p.Size == (Size{}) {
					p.Size, _ = ParseSize("A4", false)
					if j.FitTo != nil {
						p.Size = *j.FitTo
					}
				}
			case PartPadding:
				p.Pages, p.Size = 1, physicalSize
				p.Reason, p.BeforeItem = "odd_start", leaf.Ref
			case PartSource, PartPage, PartDivider:
				p.Size = leaf.Page.Size.Value
				if p.Size == (Size{}) {
					p.Size = sourceSize
					if j.FitTo != nil {
						p.Size = *j.FitTo
					}
				}
			}
			if !canvas(p.Size, leaf.Ref, padding) {
				return
			}
			if p.Pages < 1 || p.Pages > MaxGeneratedPages || layout.GeneratedPages+int64(p.Pages) > MaxGeneratedPages {
				add(diag.PlanLimit, "Generated pages exceed 100000 or the contents page count is unavailable.", leaf.Ref)
				return
			}
			physicalSize = p.Size
		}
		if int64(p.Pages) > MaxPageIndex-layout.TotalPages {
			add(diag.PlanLimit, "Packet exceeds the supported page-index range.", leaf.Ref)
			return
		}
		if j.FitTo != nil {
			physicalSize = *j.FitTo
		}
		layout.TotalPages += int64(p.Pages)
		p.LastPage = layout.TotalPages
		if leaf.Kind == PartSource {
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

	for _, e := range j.events {
		if e.leaf >= 0 {
			emit(leaves[e.leaf], false)
			continue
		}
		if e.end {
			if e.section >= 0 {
				layout.Sections[e.section].LastPage = layout.TotalPages
				active = active[:len(active)-1]
			}
			continue
		}
		if e.group.StartOn == StartOdd && layout.TotalPages%2 == 1 {
			emit(Leaf{Index: -1, Ref: e.group.Ref, Kind: PartPadding, Owner: e.owner}, true)
		}
		if e.section >= 0 {
			section := j.sections[e.section]
			section.FirstPage = layout.TotalPages + 1
			layout.Sections = append(layout.Sections, section)
			active = append(active, e.section)
		}
	}

	if len(ds) > 0 {
		return Layout{}, ds
	}
	return layout, nil
}
