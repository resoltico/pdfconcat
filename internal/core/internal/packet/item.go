// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package packet

// Ref identifies a declared item independently of its source or physical pages.
type Ref = string

// Item is a closed declaration: source, page, contents or group.
type Item interface{ itemRef() Ref }

// SourceItem selects original pages of one source PDF.
type SourceItem struct {
	Ref       Ref
	Path      string
	Selection []PageRange
}

func (i *SourceItem) itemRef() Ref { return i.Ref }

// PageItem generates Count pages with one effective style.
type PageItem struct {
	Ref   Ref
	Style PageStyle
	Count int
}

func (i *PageItem) itemRef() Ref { return i.Ref }

// ContentsItem generates linked contents pages.
type ContentsItem struct {
	Ref   Ref
	Title string
	Size  Size
}

func (i *ContentsItem) itemRef() Ref { return i.Ref }

// GroupItem scopes alignment and optionally declares a section and divider.
type GroupItem struct {
	Ref     Ref
	Title   string
	StartOn StartOn
	Divider *PageStyle
	Items   []Item
}

func (i *GroupItem) itemRef() Ref { return i.Ref }
