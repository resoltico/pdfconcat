// SPDX-License-Identifier: AGPL-3.0-only
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
	Item           string
	Pages          []packet.PageRange
	MeasureNumbers bool
	Structure      packet.StructurePolicy
	FitTo          *packet.Size
}

// Inspect reads and validates a whole source once. Each ephemeral view
// owns its containers; stream bytes stay immutable and shared during inspection.
func Inspect(c context.Context, data []byte, requests []SelectionRequest) (facts SourceFacts, occurrences []*OccurrenceFacts, ds []diag.Diagnostic) {
	occurrences = make([]*OccurrenceFacts, len(requests))
	defer func() {
		facts.SHA256 = sha256.Sum256(data)
		facts.Bytes = int64(len(data))
		if value := recover(); value != nil {
			d := diag.New(diag.SourceProcessingFailed, "The PDF backend failed unexpectedly.")
			d.Detail = fmt.Sprint(value)
			ds = append(ds, d)
		}
	}()
	s, findings, err := readSource(c, data)
	ds = append(ds, findings...)
	if err != nil {
		if s != nil && s.XRefTable != nil {
			facts.Pages = s.PageCount
			if s.HeaderVersion != nil || s.RootVersion != nil {
				facts.Version = s.XRefTable.Version().String()
			}
		}
		return facts, occurrences, append(ds, diagnostic(err, diag.SourceInvalid))
	}
	if len(requests) > 0 && requests[0].Structure == packet.StructureDiscard {
		root, e := structureRoot(s)
		if e != nil {
			return facts, occurrences, append(ds, diagnostic(e, diag.SourceInvalid))
		}
		if root != nil {
			ds = append(ds, diag.New(diag.SourceStructureRemoved, "Source structure is discarded by explicit policy."))
		}
	}
	facts, findings, err = observe(c, s)
	ds = append(ds, findings...)
	if err != nil {
		return facts, occurrences, append(ds, diagnostic(err, diag.SourceInvalid))
	}
	var sourceGeometry []pageGeometry
	for index, request := range requests {
		ranges, err := selectionRanges(request.Pages, s.PageCount)
		if err == nil && !wholeSelection(ranges, s.PageCount) && (facts.HasFields || facts.HasWidgets) {
			err = refuse(diag.SourceSelectionFormUnsupported, "Partial selection of sources containing form fields or widgets is unsupported; select the whole source or flatten it with a suitable PDF tool.")
		}
		if err != nil {
			d := diagnostic(err, diag.SourceInvalid)
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		view, err := selectionView(c, s)
		if err != nil {
			d := diagnostic(err, diag.SourceInvalid)
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		removed, err := projectSelection(c, view, ranges)
		if err != nil {
			d := diagnostic(err, diag.SourceInvalid)
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		selected, _, err := observe(c, view)
		if err != nil {
			d := diagnostic(err, diag.SourceInvalid)
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		selected.Selection = ranges
		selected.OutlineDepth, err = outlineDepth(c, view)
		if err != nil {
			d := diagnostic(err, diag.SourceInvalid)
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		if request.MeasureNumbers {
			if sourceGeometry == nil {
				sourceGeometry, err = pageGeometries(c, s)
				if err != nil {
					return facts, occurrences, append(ds, diagnostic(err, diag.SourceInvalid))
				}
			}
			if facts.PageSizes == nil {
				facts.PageSizes = map[int]packet.Size{}
			}
			for _, span := range ranges {
				for number := span.First; number <= span.Last; number++ {
					facts.PageSizes[number] = sourceGeometry[number-1].size
				}
			}
		}

		if request.Structure != packet.StructureDiscard && !facts.HasWidgets {
			root, e := structureRoot(view)
			if e == nil && root != nil {
				e = pruneStructure(c, view)
				if e == nil && request.FitTo != nil {
					e = fitContext(c, view, request.FitTo)
				}
				if e == nil && request.FitTo != nil {
					root, e = structureRoot(view)
					if e == nil {
						cat, err := view.Catalog()
						if err != nil {
							e = err
						} else {
							e = combineStructure(c, view, []structurePart{{root: root, catalog: cat}})
						}
					}
				}
				if e == nil && request.FitTo != nil {
					e = validateStructure(c, view)
				}
			}
			if e != nil {
				d := diagnostic(e, diag.SourceInvalid)
				d.Items = []string{request.Item}
				ds = append(ds, d)
				continue
			}
		}
		selectedFacts := selected.OccurrenceFacts
		occurrences[index] = &selectedFacts
		if removed {
			ds = append(ds, diag.New(diag.SourceNavigationRemoved, "Navigation targets omitted from this source occurrence were removed.", request.Item))
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
			return nil, refuse(diag.SourceSelectionOutOfRange, fmt.Sprintf("Selected range [%d,%d] is outside the source's %d pages.", span.First, span.Last, count))
		}
		if len(normalized) > 0 {
			last := &normalized[len(normalized)-1]
			if span.First <= last.Last {
				return nil, refuse(diag.SourceSelectionOutOfRange, "Selected ranges must be ascending and disjoint.")
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
	geometries, err := pageGeometries(c, s)
	if err != nil {
		return false, err
	}
	selected := []pageRef{}
	for _, span := range ranges {
		for number := span.First; number <= span.Last; number++ {
			if err := c.Err(); err != nil {
				return false, err
			}
			geometry := geometries[number-1]
			d, attrs := geometry.dict, geometry.attrs
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
			selected = append(selected, geometry.pageRef)
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
