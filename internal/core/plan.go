// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import "slices"

// PageRange selects an inclusive range in original source order.
type PageRange struct{ First, Last int }

// PageNumbers configures label eligibility, counting and physical overlay placement.
type PageNumbers struct {
	Mode     NumberingMode  `json:"mode"`
	Start    int64          `json:"start"`
	Kinds    []PartKind     `json:"kinds"`
	Position NumberPosition `json:"position"`
	Size     float64        `json:"size"`
	Margin   float64        `json:"margin"`
}

// DefaultPageNumbers returns the product defaults as an effective configuration.
func DefaultPageNumbers() *PageNumbers {
	return &PageNumbers{Mode: "physical", Start: 1, Kinds: []PartKind{PartSource, PartPage, PartDivider, PartContents}, Position: "bottom-center", Size: 9, Margin: 8 * 72 / 25.4}
}

// Plan is an ordered declaration with a single derived traversal.
type Plan struct {
	admitted     bool
	declaration  *Declaration
	metadata     *Metadata
	timestamp    *int64
	structure    StructurePolicy
	output       string
	fitTo        *Size
	pageNumbers  *PageNumbers
	pageDefaults PageStyle
	items        []Item
	leaves       []Leaf
	sections     []Section
	events       []event
	oddStarts    int
}

// Leaf is one derived source, page, divider or contents item.
type Leaf struct {
	NumberingEnabled bool
	Index            int
	Ref              Ref
	Kind             PartKind
	Owner            Ref
	Source           string
	Selection        []PageRange
	Page             *PageStyle
	Count            int
	Contents         *ContentsItem
}

type event struct {
	owner   Ref
	leaf    int
	group   *GroupItem
	section int
	end     bool
}

// newPlan derives item order and section ancestry once.
func newPlan(output string, fit *Size, numbers *PageNumbers, defaults PageStyle, items []Item) Plan {
	if numbers != nil {
		normalized := *numbers
		normalized.Kinds = slices.Clone(numbers.Kinds)
		if normalized.Mode == "" {
			normalized.Mode = "physical"
		}
		if normalized.Start == 0 {
			normalized.Start = 1
		}
		if normalized.Position == "" {
			normalized.Position = "bottom-center"
		}
		if normalized.Kinds == nil {
			normalized.Kinds = []PartKind{PartSource, PartPage, PartDivider, PartContents}
		}
		numbers = &normalized
	}
	items = cloneItems(items)
	fit = copyValue(fit)
	defaults = copyStyle(defaults)
	p := Plan{admitted: true, output: output, fitTo: fit, pageNumbers: numbers, pageDefaults: defaults, items: items}
	add := func(l Leaf) {
		l.Index = len(p.leaves)
		p.leaves = append(p.leaves, l)
		p.events = append(p.events, event{leaf: l.Index})
	}
	var walk func([]Item, Ref, int, bool)
	walk = func(items []Item, owner Ref, depth int, enabled bool) {
		for _, item := range items {
			choice := enabled
			var override *bool
			switch i := item.(type) {
			case *SourceItem:
				override = i.Numbering
			case *PageItem:
				override = i.Numbering
			case *ContentsItem:
				override = i.Numbering
			case *GroupItem:
				override = i.Numbering
			}
			if override != nil {
				choice = *override
			}
			switch i := item.(type) {
			case *SourceItem:
				add(Leaf{NumberingEnabled: choice, Ref: i.Ref, Kind: PartSource, Owner: owner, Source: i.Path, Selection: i.Selection})
			case *PageItem:
				add(Leaf{NumberingEnabled: choice, Ref: i.Ref, Kind: PartPage, Owner: owner, Page: &i.Style, Count: i.Count})
			case *ContentsItem:
				add(Leaf{NumberingEnabled: choice, Ref: i.Ref, Kind: PartContents, Owner: owner, Contents: i})
			case *GroupItem:
				section := -1
				if i.StartOn == StartOdd {
					p.oddStarts++
				}
				if i.Title != "" {
					section = len(p.sections)
					p.sections = append(p.sections, Section{Item: i.Ref, Title: i.Title, Parent: owner, Depth: depth})
				}
				p.events = append(p.events, event{leaf: -1, group: i, section: section, owner: owner})
				childOwner, childDepth := owner, depth
				if section >= 0 {
					childOwner, childDepth = i.Ref, depth+1
					if i.Divider != nil {
						style := *i.Divider
						style.Text.Value = Setting[string]{Present: true, Value: i.Title, From: i.Ref + "/title"}
						add(Leaf{NumberingEnabled: choice, Ref: dividerRef(i.Ref), Kind: PartDivider, Owner: i.Ref, Page: &style, Count: 1})
					}
				}
				walk(i.Items, childOwner, childDepth, choice)
				p.events = append(p.events, event{leaf: -1, group: i, section: section, end: true})
			}
		}
	}
	walk(items, "", 0, true)
	return p
}

// Leaves returns the derived item order.
func (p Plan) Leaves() []Leaf {
	result := append([]Leaf(nil), p.leaves...)
	for i := range result {
		l := &result[i]
		l.Selection = append([]PageRange(nil), l.Selection...)
		if l.Page != nil {
			s := copyStyle(*l.Page)
			l.Page = &s
		}
		if l.Contents != nil {
			c := *l.Contents
			c.Numbering = copyValue(c.Numbering)
			l.Contents = &c
		}
	}
	return result
}

// Sections returns the declared section ancestry in preorder.
func (p Plan) Sections() []Section { return append([]Section(nil), p.sections...) }

// Items returns an owned snapshot of authored items.
func (p Plan) Items() []Item { return cloneItems(p.items) }

// OddStarts returns the number of groups that may require padding.
func (p Plan) OddStarts() int { return p.oddStarts }

// SourceFacts contains one occurrence's selected count, boundaries and ranges.
type SourceFacts struct {
	PageSizes           []PageSizeRun
	Pages               int
	FirstSize, LastSize Size
	Selection           []PageRange
}

// SourceMapping associates a selected original range with its first output page.
type SourceMapping struct {
	SourceFirst, SourceLast int
	OutputFirst             int64
}

// Part is one compact physical span in the resolved packet.
type Part struct {
	Leaf
	Pages               int
	FirstPage, LastPage int64
	Size                Size
	Mapping             []SourceMapping
	Reason              string
	BeforeItem          Ref
}

// Section records an authored group's navigation and subtree range.
type Section struct {
	Item                        Ref
	Title                       string
	Parent                      Ref
	Depth                       int
	FirstPage, LastPage, Target int64
}

// Layout is the authoritative sequence, section hierarchy and totals.
type Layout struct {
	Language                                string
	FitTo                                   *Size
	Numbering                               []NumberSpan
	Parts                                   []Part
	Sections                                []Section
	PageNumbers                             *PageNumbers
	SourcePages, GeneratedPages, TotalPages int64
}

// FieldRename records a fully qualified field name before and after merging.
type FieldRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Metadata returns an owned document-identity snapshot.
func (p Plan) Metadata() *Metadata { return copyMetadata(p.metadata) }

// Timestamp returns an owned authored-time snapshot.
func (p Plan) Timestamp() *int64 { return copyValue(p.timestamp) }

// Structure is the admitted structure policy.
func (p Plan) Structure() StructurePolicy { return p.structure }

// Output is the resolved output destination.
func (p Plan) Output() string { return p.output }

// FitTo returns an owned fitting canvas.
func (p Plan) FitTo() *Size { return copyValue(p.fitTo) }

// PageNumbers returns an owned numbering snapshot, preserving absent/empty kinds.
func (p Plan) PageNumbers() *PageNumbers {
	n := copyValue(p.pageNumbers)
	if n != nil {
		n.Kinds = slices.Clone(n.Kinds)
	}
	return n
}

// PageDefaults returns an owned effective style snapshot.
func (p Plan) PageDefaults() PageStyle { return copyStyle(p.pageDefaults) }

// Declaration returns an owned mutable recipe, with no shared traversal state.
func (p Plan) Declaration() Declaration {
	if p.declaration != nil {
		return cloneDeclaration(*p.declaration)
	}
	return Declaration{Metadata: p.Metadata(), Timestamp: p.Timestamp(), Structure: p.Structure(), Output: p.Output(), FitTo: p.FitTo(), PageNumbers: p.PageNumbers(), PageDefaults: p.PageDefaults(), Items: p.Items()}
}

func dividerRef(ref Ref) Ref { return ref + "/divider" }

// PartIdentity binds facts to one physical span, including generated padding.
type PartIdentity struct {
	Ref       Ref
	Kind      PartKind
	FirstPage int64
}

// Identity returns the stable identity within a resolved assembly.
func (p Part) Identity() PartIdentity {
	return PartIdentity{Ref: p.Ref, Kind: p.Kind, FirstPage: p.FirstPage}
}
