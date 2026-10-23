// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
)

// SelectionRequest identifies one occurrence in a grouped source inspection.
type SelectionRequest struct {
	Item          string
	Pages         []packet.PageRange
	MeasureFooter bool
}

// InspectSelections reads and validates a whole source once. Each ephemeral view
// owns its containers; stream bytes stay immutable and shared during inspection.
func InspectSelections(c context.Context, data []byte, requests []SelectionRequest) (facts SourceFacts, occurrences map[string]OccurrenceFacts, ds []diag.Diagnostic) {
	occurrences = map[string]OccurrenceFacts{}
	defer func() {
		facts.SHA256 = sha256.Sum256(data)
		facts.Bytes = int64(len(data))
		if value := recover(); value != nil {
			ds = append(ds, diag.New("source_invalid", fmt.Sprintf("PDF parser panic: %v", value)))
		}
	}()
	s, err := read(c, data)
	if err != nil {
		if s != nil {
			facts.Pages = s.PageCount
			facts.Version = s.XRefTable.Version().String()
		}
		return facts, occurrences, []diag.Diagnostic{diagnostic(err, "source_invalid")}
	}
	facts, ds, err = observe(c, s)
	if err != nil {
		return facts, occurrences, append(ds, diagnostic(err, "source_invalid"))
	}
	for _, request := range requests {
		ranges, err := selectionRanges(request.Pages, s.PageCount)
		if err == nil && !wholeSelection(ranges, s.PageCount) && (facts.HasFields || facts.HasWidgets) {
			err = refuse("source_selection_form_unsupported", "Partial selection of sources containing form fields or widgets is unsupported; select the whole source or flatten it with a suitable PDF tool.")
		}
		if err != nil {
			d := diagnostic(err, "source_invalid")
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		view, err := selectionView(c, s)
		if err != nil {
			d := diagnostic(err, "source_invalid")
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		removed, err := projectSelection(c, view, ranges)
		if err != nil {
			d := diagnostic(err, "source_invalid")
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		selected, _, err := observe(c, view)
		if err != nil {
			d := diagnostic(err, "source_invalid")
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		selected.Selection = ranges
		selected.OutlineDepth, err = outlineDepth(c, view)
		if err != nil {
			d := diagnostic(err, "source_invalid")
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		if request.MeasureFooter {
			measured := map[int]packet.Size{}
			for _, span := range ranges {
				for number := span.First; number <= span.Last; number++ {
					if _, known := facts.PageSizes[number]; known {
						continue
					}
					size, e := displayedSize(c, s, number)
					if e != nil {
						err = e
						break
					}
					measured[number] = size
				}
				if err != nil {
					break
				}
			}
			if err != nil {
				d := diagnostic(err, "source_invalid")
				d.Items = []string{request.Item}
				ds = append(ds, d)
				continue
			}
			if facts.PageSizes == nil {
				facts.PageSizes = map[int]packet.Size{}
			}
			for number, size := range measured {
				facts.PageSizes[number] = size
			}
		}
		occurrences[request.Item] = selected.OccurrenceFacts
		if removed {
			ds = append(ds, diag.New("source_navigation_removed", "Navigation targets omitted from this source occurrence were removed.", request.Item))
		}
	}
	return facts, occurrences, ds
}

func selectionRanges(ranges []packet.PageRange, count int) ([]packet.PageRange, error) {
	if len(ranges) == 0 {
		return []packet.PageRange{{First: 1, Last: count}}, nil
	}
	normalized := []packet.PageRange{}
	for _, span := range ranges {
		if span.First < 1 || span.Last < span.First || span.Last > count {
			return nil, refuse("source_selection_out_of_range", fmt.Sprintf("Selected range [%d,%d] is outside the source's %d pages.", span.First, span.Last, count))
		}
		if len(normalized) > 0 {
			last := &normalized[len(normalized)-1]
			if span.First <= last.Last {
				return nil, refuse("source_selection_out_of_range", "Selected ranges must be ascending and disjoint.")
			}
			if span.First == last.Last+1 {
				last.Last = span.Last
				continue
			}
		}
		normalized = append(normalized, span)
	}
	return normalized, nil
}
func wholeSelection(ranges []packet.PageRange, count int) bool {
	return len(ranges) == 1 && ranges[0].First == 1 && ranges[0].Last == count
}

func selectionView(c context.Context, s *model.Context) (*model.Context, error) {
	view := *s
	xref := *s.XRefTable
	view.XRefTable = &xref
	xref.Table = make(map[int]*model.XRefTableEntry, len(s.Table))
	for n, entry := range s.Table {
		if err := c.Err(); err != nil {
			return nil, err
		}
		if entry == nil {
			continue
		}
		copyEntry := *entry
		if entry.Object != nil {
			copyEntry.Object = entry.Object.Clone()
		}
		xref.Table[n] = &copyEntry
	}
	if s.Size != nil {
		size := *s.Size
		xref.Size = &size
	}
	xref.RootDict = nil
	xref.Names = map[string]*model.Node{}
	xref.Dests = nil
	xref.NameRefs = map[string]model.NameMap{}
	xref.Outlines = nil
	return &view, nil
}

// projectSelection keeps original page identities and materializes inherited
// attributes before replacing their ancestors. It is used on isolated contexts.
func projectSelection(c context.Context, s *model.Context, ranges []packet.PageRange) (bool, error) {
	ranges, err := selectionRanges(ranges, s.PageCount)
	if err != nil {
		return false, err
	}
	if wholeSelection(ranges, s.PageCount) {
		return pruneNavigation(c, s)
	}
	selected := []pageRef{}
	for _, span := range ranges {
		for number := span.First; number <= span.Last; number++ {
			if err := c.Err(); err != nil {
				return false, err
			}
			d, ref, attrs, err := s.PageDict(c, number, false)
			if err != nil {
				return false, err
			}
			if attrs.Resources != nil {
				d["Resources"] = attrs.Resources.Clone()
			}
			d["MediaBox"] = attrs.MediaBox.Array()
			if attrs.CropBox != nil {
				d["CropBox"] = attrs.CropBox.Array()
			} else {
				delete(d, "CropBox")
			}
			d["Rotate"] = types.Integer(attrs.Rotate)
			selected = append(selected, pageRef{ref: *ref, dict: d})
		}
	}
	root, err := s.Pages()
	if err != nil {
		return false, err
	}
	var makeTree func([]pageRef, types.IndirectRef) (types.Dict, error)
	makeTree = func(pages []pageRef, parent types.IndirectRef) (types.Dict, error) {
		d := types.Dict{"Type": types.Name("Pages"), "Count": types.Integer(len(pages))}
		kids := types.Array{}
		if len(pages) <= 64 {
			for _, p := range pages {
				p.dict["Parent"] = parent
				kids = append(kids, p.ref)
			}
		} else {
			span := (len(pages) + 63) / 64
			for first := 0; first < len(pages); first += span {
				ref, err := s.IndRefForNewObject(types.Dict{})
				if err != nil {
					return nil, err
				}
				child, err := makeTree(pages[first:min(first+span, len(pages))], *ref)
				if err != nil {
					return nil, err
				}
				child["Parent"] = parent
				s.Table[ref.ObjectNumber.Value()].Object = child
				kids = append(kids, *ref)
			}
		}
		d["Kids"] = kids
		return d, nil
	}
	tree, err := makeTree(selected, *root)
	if err != nil {
		return false, err
	}
	s.Table[root.ObjectNumber.Value()].Object = tree
	s.PageCount = len(selected)
	s.PageAnnots = map[int]model.PgAnnots{}
	s.PageThumbs = map[int]types.IndirectRef{}
	s.URIs = map[int]map[string]string{}
	s.Valid = false
	removed, err := pruneNavigation(c, s)
	if err != nil {
		return false, err
	}
	// Omitted pages remain private objects; selection is composition, not redaction.
	return removed, nil
}
