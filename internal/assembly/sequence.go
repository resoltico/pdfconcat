// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins

// Package assembly defines PDFConcat's ordered PDF/blank domain model.
package assembly

import (
	"errors"
	"fmt"
)

// ItemKind identifies a sequence item as a PDF or generated blank pages.
type ItemKind uint8

// PDF and Blank are the two supported sequence item kinds.
const (
	PDF ItemKind = iota + 1
	Blank
)

// MaxBlankCount bounds the number of consecutive blanks one item may request, so that a typo
// such as an extra digit cannot ask for billions of pages.
const MaxBlankCount = 1_000_000

// Item is one entry of the ordered sequence: a source PDF, or Count
// consecutive generated blank pages sharing one style.
type Item struct {
	Kind  ItemKind
	Path  string     // PDF only.
	Blank BlankStyle // Blank only; layered over the run-wide defaults.
	Count int        // Blank only; at least 1.
}

// PDFItem returns a sequence item for a source PDF.
func PDFItem(path string) Item {
	return Item{Kind: PDF, Path: path}
}

// BlankItem returns a sequence item for count generated blank pages.
func BlankItem(style BlankStyle, count int) Item {
	return Item{Kind: Blank, Blank: style, Count: count}
}

// Sequence is the ordered assembly description consumed by PDFConcat.
type Sequence struct {
	Items []Item
}

// Validate checks structural sequence invariants.
func (s Sequence) Validate() error {
	if len(s.Items) == 0 {
		return errors.New("assembly sequence is empty")
	}

	pdfs := 0

	for index := range s.Items {
		item := &s.Items[index]

		switch item.Kind {
		case PDF:
			if item.Path == "" {
				return fmt.Errorf("sequence item %d has an empty PDF path", index+1)
			}

			pdfs++
		case Blank:
			if item.Count < 1 || item.Count > MaxBlankCount {
				return fmt.Errorf("sequence item %d is a blank with count %d; want 1 to %d", index+1, item.Count, MaxBlankCount)
			}
		default:
			return fmt.Errorf("sequence item %d has an unknown kind", index+1)
		}
	}

	if pdfs == 0 {
		return errors.New("assembly sequence contains no PDF inputs")
	}

	return nil
}

// DistinctPDFPaths returns each source path once, in first-use order.
func (s Sequence) DistinctPDFPaths() []string {
	seen := make(map[string]struct{}, len(s.Items))

	paths := make([]string, 0, len(s.Items))
	for index := range s.Items {
		item := &s.Items[index]
		if item.Kind != PDF {
			continue
		}

		if _, found := seen[item.Path]; found {
			continue
		}

		seen[item.Path] = struct{}{}
		paths = append(paths, item.Path)
	}

	return paths
}

// HasBlank reports whether the sequence contains any blank item.
func (s Sequence) HasBlank() bool {
	for index := range s.Items {
		if s.Items[index].Kind == Blank {
			return true
		}
	}

	return false
}
