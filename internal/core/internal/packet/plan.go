// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package packet

// PageRange selects an inclusive range in original source order.
type PageRange struct{ First, Last int }

// PageNumbers configures physical Arabic footer numbers.
type PageNumbers struct {
	Size   float64 `json:"size"`
	Margin float64 `json:"margin"`
}

// Plan is an ordered declaration with a single derived traversal.
type Plan struct {
	Output       string
	FitTo        *Size
	PageNumbers  *PageNumbers
	PageDefaults PageStyle
	Items        []Item
	leaves       []Leaf
	sections     []Section
	events       []event
	oddStarts    int
}

// Leaf is one derived source, page, divider or contents item.
type Leaf struct {
	Index     int
	Ref       Ref
	Kind      PartKind
	Owner     Ref
	Source    string
	Selection []PageRange
	Page      *PageStyle
	Count     int
	Contents  *ContentsItem
}

type event struct {
	owner   Ref
	leaf    int
	group   *GroupItem
	section int
	end     bool
}

// NewPlan derives item order and section ancestry once.
func NewPlan(output string, fit *Size, numbers *PageNumbers, defaults PageStyle, items []Item) Plan {
	p := Plan{Output: output, FitTo: fit, PageNumbers: numbers, PageDefaults: defaults, Items: items}
	add := func(l Leaf) {
		l.Index = len(p.leaves)
		p.leaves = append(p.leaves, l)
		p.events = append(p.events, event{leaf: l.Index})
	}
	var walk func([]Item, Ref, int)
	walk = func(items []Item, owner Ref, depth int) {
		for _, item := range items {
			switch i := item.(type) {
			case *SourceItem:
				add(Leaf{Ref: i.Ref, Kind: PartSource, Owner: owner, Source: i.Path, Selection: i.Selection})
			case *PageItem:
				add(Leaf{Ref: i.Ref, Kind: PartPage, Owner: owner, Page: &i.Style, Count: i.Count})
			case *ContentsItem:
				add(Leaf{Ref: i.Ref, Kind: PartContents, Owner: owner, Contents: i})
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
						style.Text.Value = Setting[string]{Value: i.Title, From: i.Ref + "/title"}
						add(Leaf{Ref: i.Ref + "/divider", Kind: PartDivider, Owner: i.Ref, Page: &style, Count: 1})
					}
				}
				walk(i.Items, childOwner, childDepth)
				p.events = append(p.events, event{leaf: -1, group: i, section: section, end: true})
			}
		}
	}
	walk(items, "", 0)
	return p
}

// Leaves returns the derived item order.
func (p Plan) Leaves() []Leaf { return p.leaves }

// Sections returns the declared section ancestry in preorder.
func (p Plan) Sections() []Section { return p.sections }

// OddStarts returns the number of groups that may require padding.
func (p Plan) OddStarts() int { return p.oddStarts }

// SourceFacts contains one occurrence's selected count, boundaries and ranges.
type SourceFacts struct {
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
