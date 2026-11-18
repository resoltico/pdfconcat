// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package packet

import (
	"errors"
	"fmt"
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
	start := n.First()
	if start < 1 || start > MaxPageIndex {
		return errors.New("page-number start exceeds the supported page-index range")
	}
	consecutive := n.Mode == "consecutive"
	numbered := int64(0)
	for _, p := range layout.Parts {
		span := NumberSpan{Output: [2]int64{p.FirstPage, p.LastPage}}
		if n.Eligible(p.Kind, p.NumberingEnabled) {
			offset := p.FirstPage - 1
			if consecutive {
				offset = numbered
			}
			additional := p.LastPage - p.FirstPage
			limit := MaxPageIndex - start
			if offset+additional > limit {
				firstOffending := p.FirstPage + max(0, limit+1-offset)
				return fmt.Errorf("page numbering exceeds the limit at physical page %d. Set page_numbers.start to at most %d in %s mode",
					firstOffending, largestNumberingStart(layout.Parts, n), n.Mode)
			}
			first := start + offset
			span.Start = &first
			numbered += additional + 1
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

func largestNumberingStart(parts []Part, n PageNumbers) int64 {
	var numbered, last int64
	for _, part := range parts {
		if n.Eligible(part.Kind, part.NumberingEnabled) {
			last = part.LastPage
			numbered += part.LastPage - part.FirstPage + 1
		}
	}
	if n.Mode == "consecutive" {
		last = numbered
	}
	return MaxPageIndex - (last - 1)
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
