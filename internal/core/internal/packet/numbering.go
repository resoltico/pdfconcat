// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"errors"
	"slices"
	"strconv"
)

// NumberSpan records one physical range and its first authored numeric label.
// Nil Start means no label and no painted overlay on the entire span.
type NumberSpan struct {
	Output [2]int64 `json:"output"`
	Start  *int64   `json:"start"`
}

// Eligible applies inherited item choice, then kind eligibility. Padding is
// never eligible. Nil Kinds means all authored kinds; an empty slice means none.
func (n PageNumbers) Eligible(kind PartKind, enabled bool) bool {
	return enabled && kind != PartPadding && (n.Kinds == nil || slices.Contains(n.Kinds, kind))
}

// First is the defaulted starting number.
func (n PageNumbers) First() int64 {
	if n.Start == 0 {
		return 1
	}
	return n.Start
}

func resolveNumbering(layout *Layout) error {
	layout.Numbering = []NumberSpan{}
	if layout.PageNumbers == nil {
		return nil
	}
	n := *layout.PageNumbers
	next := n.First()
	if next < 1 || next > MaxPageIndex {
		return errors.New("page-number start exceeds the supported page-index range")
	}
	exhausted := false
	for _, p := range layout.Parts {
		span := NumberSpan{Output: [2]int64{p.FirstPage, p.LastPage}}
		if n.Eligible(p.Kind, p.NumberingEnabled) {
			if n.Mode == "consecutive" && exhausted {
				return errors.New("consecutive page numbering overflows")
			}
			first := next
			if n.Mode != "consecutive" {
				if p.FirstPage-1 > MaxPageIndex-n.First() {
					return errors.New("physical page numbering overflows")
				}
				first = n.First() + (p.FirstPage - 1)
			}
			count := p.LastPage - p.FirstPage
			if count > MaxPageIndex-first {
				return errors.New("page numbering overflows")
			}
			span.Start = &first
			if n.Mode == "consecutive" {
				next = first + count
				if next < MaxPageIndex {
					next++
				} else {
					exhausted = true
				}
			}
		}
		if len(layout.Numbering) > 0 {
			last := &layout.Numbering[len(layout.Numbering)-1]
			adjacent := last.Output[1]+1 == span.Output[0]
			continuous := last.Start == nil && span.Start == nil
			if last.Start != nil && span.Start != nil {
				continuous = *span.Start-*last.Start == span.Output[0]-last.Output[0]
			}
			if adjacent && continuous {
				last.Output[1] = span.Output[1]
				continue
			}
		}
		layout.Numbering = append(layout.Numbering, span)
	}
	return nil
}

// NumberAt returns the single resolved numeric mapping used by all consumers.
func NumberAt(spans []NumberSpan, page int64) (int64, bool) {
	index, found := slices.BinarySearchFunc(spans, page, func(s NumberSpan, p int64) int {
		if s.Output[1] < p {
			return -1
		}
		if s.Output[0] > p {
			return 1
		}
		return 0
	})
	if !found || spans[index].Start == nil {
		return 0, false
	}
	span := spans[index]
	return *span.Start + (page - span.Output[0]), true
}

// LabelAt uses decimal labels or the documented unnumbered-target em dash.
func (l Layout) LabelAt(page int64) string {
	if l.PageNumbers == nil {
		return strconv.FormatInt(page, 10)
	}
	if n, ok := NumberAt(l.Numbering, page); ok {
		return strconv.FormatInt(n, 10)
	}
	return "—"
}
