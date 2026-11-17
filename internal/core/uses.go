// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

// TextUse is an effective font/text dependency with all declaration sites.
type TextUse struct {
	Leaf int
	Font Font
	Text string
	Refs []string
}

// TextUses enumerates consumed fonts, including fonts checked for empty text.
// Unconsumed defaults do not create dependencies.
func (p Plan) TextUses() []TextUse {
	var uses []TextUse
	current := -1
	add := func(font Font, text string, refs ...string) {
		uses = append(uses, TextUse{Leaf: current, Font: font, Text: text, Refs: refs})
	}
	leaves, sections := p.Leaves(), p.Sections()
	for _, leaf := range leaves {
		current = leaf.Index
		if leaf.Page != nil {
			add(leaf.Page.Text.Font.Value, leaf.Page.Text.Value.Value, leaf.Page.Text.Value.From, leaf.Page.Text.Font.From, leaf.Ref)
		}
		if leaf.Contents != nil {
			font := p.PageDefaults().Text.Font
			add(font.Value, leaf.Contents.Title, leaf.Ref, font.From)
			for _, section := range sections {
				add(font.Value, section.Title, section.Item+"/title", leaf.Ref, font.From)
			}
			// Layout measures all decimal cells and the unnumbered-target label.
			add(font.Value, "0123456789—", leaf.Ref, font.From)
		}
	}
	current = -1
	if p.PageNumbers() != nil {
		add(Font{}, "0123456789", "/page_numbers")
	}
	return uses
}

// InputResource retains first-use order and the declarations consuming a path.
type InputResource struct {
	Path      string
	Consumers []Ref
}

// Inputs is the single inventory of resolved source and effective font resources.
func (p Plan) Inputs() []InputResource {
	var resources []InputResource
	indices := map[string]int{}
	consumers := map[string]map[Ref]bool{}
	add := func(path string, refs ...Ref) {
		if path == "" {
			return
		}
		index, exists := indices[path]
		if !exists {
			index = len(resources)
			indices[path] = index
			resources = append(resources, InputResource{Path: path})
			consumers[path] = map[Ref]bool{}
		}
		for _, ref := range refs {
			if ref == "" {
				continue
			}
			if !consumers[path][ref] {
				resources[index].Consumers = append(resources[index].Consumers, ref)
				consumers[path][ref] = true
			}
		}
	}
	uses := p.TextUses()
	next := 0
	for _, leaf := range p.Leaves() {
		if leaf.Kind == PartSource {
			add(leaf.Source, leaf.Ref)
		}
		for next < len(uses) && uses[next].Leaf == leaf.Index {
			use := uses[next]
			add(use.Font.Path, use.Refs...)
			next++
		}
	}
	for _, use := range uses[next:] {
		add(use.Font.Path, use.Refs...)
	}
	return resources
}
