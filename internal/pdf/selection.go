// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strconv"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
)

// SelectionRequest identifies one occurrence in a grouped source inspection.
type SelectionRequest struct {
	Item    string
	Pages   []packet.PageRange
	Fitting *packet.FitPolicy
}

// Inspect normally reads and validates the whole source once. A possible
// zero-offset structure index can trigger one discard replay for a preserve
// refusal hint. Each ephemeral view owns its containers, with stream bytes
// immutable and shared during inspection.
func Inspect(c context.Context, data []byte, policy packet.StructurePolicy, requests []SelectionRequest) (facts SourceFacts, occurrences []*OccurrenceFacts, ds []diag.Diagnostic) {
	return InspectReader(c, bytes.NewReader(data), sha256.Sum256(data), int64(len(data)), SourcePolicies{Structure: policy}, requests)
}

// InspectReader admits a seekable snapshot with its acquisition facts.
func InspectReader(c context.Context, reader io.ReadSeeker, digest [32]byte, size int64, policies SourcePolicies, requests []SelectionRequest) (facts SourceFacts, occurrences []*OccurrenceFacts, ds []diag.Diagnostic) {
	occurrences = make([]*OccurrenceFacts, len(requests))
	defer func() {
		facts.SHA256 = digest
		facts.Bytes = size
		if value := recover(); value != nil {
			d := diag.Report(diag.SourceProcessingFailed, diag.ThePDFBackendFailedUnexpectedly())
			d.Detail = fmt.Sprint(value)
			ds = append(ds, d)
		}
	}()
	s, origins, findings, err := readSourceReader(c, reader, policies)
	policy := policies.Structure
	ds = append(ds, findings...)
	if err != nil {
		if s != nil && s.XRefTable != nil {
			facts.Pages = s.PageCount
			if s.HeaderVersion != nil || s.RootVersion != nil {
				facts.Version = s.XRefTable.Version().String()
			}
		}
		return facts, occurrences, append(ds, diagnostic(proveZeroOffsetDiscardHint(c, reader, policies, err), diag.SourceInvalid))
	}
	facts, findings, err = observeSource(c, s, origins)
	ds = append(ds, findings...)
	if err != nil {
		return facts, occurrences, append(ds, diagnostic(err, diag.SourceInvalid))
	}
	if facts.UnboundMCIDLocation != nil {
		reported := false
		for _, d := range ds {
			if d.Code == diag.SourceStructureRemoved || d.Code == diag.SourceStructureOrphaned {
				reported = true
				break
			}
		}
		if !reported {
			code := diag.SourceStructureOrphaned
			message := diag.UnboundMarkedContentIDsSupplyNoHierarchyMeaningfulSourceContent()
			if policy == packet.StructureDiscard {
				code = diag.SourceStructureRemoved
				message = diag.UnboundSourceMarkedContentIDsAreIgnoredByExplicitStructure()
			}
			d := diag.Report(code, message)
			d.PDFLocation = facts.UnboundMCIDLocation
			ds = append(ds, d)
		}
	}
	if len(requests) == 0 {
		uri, e := inspectExternalActions(c, s, origins)
		ds = append(ds, uri...)
		if e != nil {
			return facts, occurrences, append(ds, diagnostic(e, diag.SourceInvalid))
		}
	}
	type analysisKey struct {
		ranges string
		fitted bool
		policy packet.FitPolicy
	}
	type analysis struct {
		facts    *OccurrenceFacts
		findings []diag.Diagnostic
	}
	cache := make(map[analysisKey]analysis)
	for index, request := range requests {
		if err := c.Err(); err != nil {
			return facts, occurrences, append(ds, diagnostic(err, diag.SourceInvalid))
		}
		ranges, err := selectionRanges(request.Pages, s.PageCount)
		if err == nil && !wholeSelection(ranges, s.PageCount) && (facts.HasFields || facts.HasWidgets) {
			err = refuse(diag.SourceSelectionFormUnsupported, diag.PartialSelectionOfSourcesContainingFormFieldsOrWidgetsIs())
		}
		var key analysisKey
		var fitting *packet.FitPolicy
		if err == nil && request.Fitting != nil {
			normalized, e := request.Fitting.Normalize()
			err = e
			if err == nil {
				fitting = &normalized
				key.fitted, key.policy = true, normalized
			}
		}
		if err != nil {
			d := diagnostic(err, diag.SourceInvalid)
			d.Items = []string{request.Item}
			ds = append(ds, d)
			continue
		}
		var encoded []byte
		for _, span := range ranges {
			encoded = strconv.AppendInt(encoded, int64(span.First), 10)
			encoded = append(encoded, '-')
			encoded = strconv.AppendInt(encoded, int64(span.Last), 10)
			encoded = append(encoded, ',')
		}
		key.ranges = string(encoded)
		analyzed, known := cache[key]
		if !known {
			analyzed.facts, analyzed.findings = analyzeSelection(c, s, origins, ranges, fitting, policy)
			cache[key] = analyzed
		}
		occurrences[index] = analyzed.facts
		for _, finding := range analyzed.findings {
			finding.Items = []string{request.Item}
			ds = append(ds, finding)
		}
	}
	return facts, occurrences, ds
}

func analyzeSelection(c context.Context, s *model.Context, origins sourceOrigins, ranges []packet.PageRange, fitting *packet.FitPolicy, policy packet.StructurePolicy) (facts *OccurrenceFacts, ds []diag.Diagnostic) {
	view, err := selectionView(c, s)
	if err != nil {
		d := diagnostic(err, diag.SourceInvalid)
		ds = append(ds, d)
		return facts, ds
	}
	projection, err := projectSelection(c, view, ranges, origins, false)
	if err != nil {
		d := diagnostic(err, diag.SourceInvalid)
		ds = append(ds, d)
		return facts, ds
	}
	ds = append(ds, navigationDiagnostics(projection.removed)...)
	ds = append(ds, projection.external...)
	geometries, e := pageGeometries(c, view)
	if e != nil {
		d := diagnostic(e, diag.SourceInvalid)
		ds = append(ds, d)
		return facts, ds
	}
	selected, _, err := observe(c, view, origins, fitting, geometries)
	if err != nil {
		d := diagnostic(err, diag.SourceInvalid)
		ds = append(ds, d)
		return facts, ds
	}
	selected.Selection = ranges
	if selected.UncoveredLocation != nil && selected.UncoveredLocation.Page > 0 {
		loc := *selected.UncoveredLocation
		loc.Page = originalSelectionPage(ranges, loc.Page)
		selected.UncoveredLocation = &loc
	}
	selected.PDF20AppearanceIssue.Site.Page = originalSelectionPage(ranges, selected.PDF20AppearanceIssue.Site.Page)
	selected.OutlineDepth, err = outlineDepth(c, view)
	if err != nil {
		d := diagnostic(err, diag.SourceInvalid)
		ds = append(ds, d)
		return facts, ds
	}

	// Publish complete metric knowledge before any later compatibility/rehearsal refusal.
	selectedFacts := selected.OccurrenceFacts
	facts = &selectedFacts
	selectedFacts.fitFindings, selectedFacts.AnnotationFittingIssue, e = fittingFindings(c, view, geometries, selectedFacts.fitting, origins)
	if e != nil {
		d := diagnostic(e, diag.SourceInvalid)
		ds = append(ds, d)
		return facts, ds
	}
	navigation, e := planFittedNavigation(c, view, geometries, selectedFacts.fitting, origins)
	if e != nil {
		return facts, append(ds, diagnostic(e, diag.SourceInvalid))
	}
	selectedFacts.fitFindings = append(selectedFacts.fitFindings, navigation.findings...)
	ds = append(ds, selectedFacts.fitFindings...)
	if diag.Outcome(selectedFacts.fitFindings) != "ok" {
		return facts, ds
	}
	var tagged bool
	if policy != packet.StructureDiscard {
		var root types.Dict
		root, e = structureRoot(view)
		tagged = root != nil
		if e == nil && tagged {
			e = pruneStructure(c, view)
		}
	}
	if e == nil && fitting != nil && (tagged || len(origins.undefinedReferences) > 0) {
		e = applyFitting(c, view, geometries, selectedFacts.fitting, navigation)
	}
	if e == nil {
		e = checkRetainedSourceReferences(c, view, origins)
	}
	if e == nil && tagged && fitting != nil {
		var root types.Dict
		root, e = structureRoot(view)
		if e == nil {
			var part structurePart
			part, e = captureStructurePart(view, root)
			if e == nil {
				e = combineStructure(c, view, []structurePart{part})
			}
		}
		if e == nil {
			e = validateStructure(c, view)
		}
	}
	if e != nil {
		ds = append(ds, diagnostic(e, diag.SourceInvalid))
	}

	return facts, ds
}

func selectionRanges(ranges []packet.PageRange, count int) ([]packet.PageRange, error) {
	if len(ranges) == 0 {
		return []packet.PageRange{{First: 1, Last: count}}, nil
	}
	normalized := []packet.PageRange{}
	for _, span := range ranges {
		if span.First < 1 || span.Last < span.First || span.Last > count {
			return nil, refuse(diag.SourceSelectionOutOfRange, diag.SelectedRangeDDIsOutsideTheSourceSD(int64(span.First), int64(span.Last), int64(count)))
		}
		if len(normalized) > 0 {
			last := &normalized[len(normalized)-1]
			if span.First <= last.Last {
				return nil, refuse(diag.SourceSelectionOutOfRange, diag.SelectedRangesMustBeAscendingAndDisjoint())
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
	if s.DirectInfoDict != nil {
		xref.DirectInfoDict = clonedDirectDict(s.DirectInfoDict)
	}
	xref.Names = map[string]*model.Node{}
	xref.Dests = nil
	xref.NameRefs = map[string]model.NameMap{}
	xref.Outlines = nil
	return &view, nil
}

// projectSelection keeps original page identities and materializes inherited
// attributes before replacing their ancestors. It is used on isolated contexts.
func projectSelection(c context.Context, s *model.Context, ranges []packet.PageRange, origins sourceOrigins, assembly bool) (navigationProjection, error) {
	ranges, err := selectionRanges(ranges, s.PageCount)
	if err != nil {
		return navigationProjection{}, err
	}
	if wholeSelection(ranges, s.PageCount) {
		return pruneNavigation(c, s, origins, assembly)
	}
	geometries, err := pageGeometries(c, s)
	if err != nil {
		return navigationProjection{}, err
	}
	selected := []pageRef{}
	for _, span := range ranges {
		for number := span.First; number <= span.Last; number++ {
			if err := c.Err(); err != nil {
				return navigationProjection{}, err
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
		return navigationProjection{}, err
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
		return navigationProjection{}, err
	}
	s.Table[root.ObjectNumber.Value()].Object = tree
	s.PageCount = len(selected)
	s.PageAnnots = map[int]model.PgAnnots{}
	s.PageThumbs = map[int]types.IndirectRef{}
	s.URIs = map[int]map[string]string{}
	s.Valid = false
	projection, err := pruneNavigation(c, s, origins, assembly)
	if err != nil {
		return navigationProjection{}, err
	}
	// Omitted pages remain private objects; selection is composition, not redaction.
	return projection, nil
}

// originalSelectionPage projects a selected view's location back to its input.
// Zero remains unknown; the selected ordinal stays separate in the issue facts.
func originalSelectionPage(ranges []packet.PageRange, page int) int {
	if page <= 0 {
		return 0
	}
	for _, span := range ranges {
		count := span.Last - span.First + 1
		if page <= count {
			return span.First + page - 1
		}
		page -= count
	}
	panic("selected location is outside its admitted selection")
}
