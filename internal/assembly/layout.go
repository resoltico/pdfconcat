// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

package assembly

import (
	"errors"
	"fmt"
)

// DocumentInfo is what assembly needs to know about a validated source PDF.
type DocumentInfo struct {
	Pages     int
	FirstPage PageDim
	LastPage  PageDim
}

// Part is one resolved, ordered contribution to the output.
type Part struct {
	Kind      ItemKind
	Path      string    // PDF only.
	Blank     BlankSpec // Blank only.
	Pages     int       // PDF page count, or number of blank pages.
	FirstPage int       // 1-based output page number of the first contributed page.
}

// Layout is the backend-ready ordered parts plus page accounting.
type Layout struct {
	Parts       []Part
	SourcePages int
	BlankPages  int
	TotalPages  int
}

// BuildLayout resolves a Sequence into ordered parts. Blank styles are
// layered over defaults; an inherited blank page size is that of the nearest
// preceding source page, or of the first source page for leading blanks.
func BuildLayout(sequence Sequence, defaults BlankStyle, documents map[string]DocumentInfo) (Layout, error) {
	err := sequence.Validate()
	if err != nil {
		return Layout{}, err
	}

	inherited, err := leadingPageDim(sequence, documents)
	if err != nil {
		return Layout{}, err
	}

	layout := Layout{Parts: make([]Part, 0, len(sequence.Items))}
	for index := range sequence.Items {
		item := &sequence.Items[index]
		part := Part{Kind: item.Kind, FirstPage: layout.TotalPages + 1}

		switch item.Kind {
		case PDF:
			document := documents[item.Path]
			part.Path, part.Pages = item.Path, document.Pages
			inherited = document.LastPage
			layout.SourcePages += document.Pages
		case Blank:
			spec, resolveErr := item.Blank.Over(defaults).Resolve(inherited)
			if resolveErr != nil {
				return Layout{}, fmt.Errorf("sequence item %d (blank): %w", index+1, resolveErr)
			}

			part.Blank, part.Pages = spec, item.Count
			layout.BlankPages += item.Count
		default:
			return Layout{}, fmt.Errorf("sequence item %d has an unknown kind", index+1)
		}

		layout.Parts = append(layout.Parts, part)
		layout.TotalPages += part.Pages
	}

	return layout, nil
}

// leadingPageDim returns the first source page size, which leading blanks inherit,
// and verifies that every source PDF has usable document information.
func leadingPageDim(sequence Sequence, documents map[string]DocumentInfo) (PageDim, error) {
	var first PageDim

	found := false

	for index := range sequence.Items {
		item := &sequence.Items[index]
		if item.Kind != PDF {
			continue
		}

		document, ok := documents[item.Path]
		if !ok {
			return PageDim{}, fmt.Errorf("missing document information for %q", item.Path)
		}

		if document.Pages <= 0 {
			return PageDim{}, fmt.Errorf("PDF %q has no pages", item.Path)
		}

		if !found {
			first, found = document.FirstPage, true
		}
	}

	if !found {
		return PageDim{}, errors.New("assembly sequence contains no PDF inputs")
	}

	return first, nil
}
