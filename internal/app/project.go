// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"fmt"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/outcome"
)

func project(req Request, a *admission, inspected *inspection, composed *composition, built *buildOutcome) Result {
	r := outcome.New(string(req.Operation))
	if a.roles.Output != "" {
		r.Output = &outcome.Output{Path: a.roles.Output}
	}
	if a.roles.Report != "" {
		r.Report = &outcome.Report{Path: a.roles.Report}
	}
	r.Diagnostics = append(r.Diagnostics, a.diagnostics...)
	if inspected != nil {
		r.Diagnostics = append(r.Diagnostics, inspected.diagnostics...)
	}
	if composed != nil {
		r.Diagnostics = append(r.Diagnostics, composed.diagnostics...)
	}
	if built != nil {
		r.Diagnostics = append(r.Diagnostics, built.diagnostics...)
		r.Output = &built.output
	}
	if a.plan == nil {
		return r
	}
	r.Metadata = a.plan.Metadata()
	r.Timestamp = a.plan.Timestamp()
	r.Structure = &outcome.Structure{Policy: a.plan.Structure()}
	r.FitTo = a.plan.FitTo()
	r.PageNumbers = a.plan.PageNumbers()
	r.Pages = &outcome.Pages{}
	r.Sections = reportSections(a.plan.Sections(), false)
	for _, leaf := range a.plan.Leaves() {
		entry := declaredReportPart(leaf)
		inspectedSource(&entry, leaf.Index, inspected)
		r.Parts = append(r.Parts, entry)
	}
	if composed == nil || composed.layout == nil {
		return r
	}
	layout := composed.layout
	r.Numbering = &layout.Numbering
	if composed.assembly.Coverage != "" {
		coverage := composed.assembly.Coverage
		r.Structure.Coverage = &coverage
	}
	r.Pages = &outcome.Pages{Source: knownCount(layout.SourcePages), Generated: knownCount(layout.GeneratedPages), Total: knownCount(layout.TotalPages)}
	r.Sections = reportSections(layout.Sections, true)
	r.Parts = make([]outcome.Part, len(composed.assembly.Parts))
	for i, part := range composed.assembly.Parts {
		entry := declaredReportPart(part.Leaf)
		inspectedSource(&entry, part.Index, inspected)
		entry.Pages = knownCount(int64(part.Pages))
		entry.OutputPages = &[2]int64{part.FirstPage, part.LastPage}
		entry.Reason = part.Reason
		entry.BeforeItem = part.BeforeItem
		if part.Kind == packet.PartSource {
			spans := make([]outcome.Span, 0, len(part.Mapping))
			for _, mapping := range part.Mapping {
				spans = append(spans, outcome.Span{Source: [2]int{mapping.SourceFirst, mapping.SourceLast}, Output: [2]int64{mapping.OutputFirst, mapping.OutputFirst + int64(mapping.SourceLast-mapping.SourceFirst)}})
			}
			entry.Spans = &spans
		} else {
			size := part.Size
			entry.Size = &size
		}
		if text := part.Text; text != nil {
			entry.Text = &outcome.Text{Font: text.Font, FontSize: text.FontSize, Bounds: text.Bounds, InkBounds: text.InkBounds, Overflow: text.Overflow}
		}
		entry.FieldsRenamed = part.FieldsRenamed
		r.Parts[i] = entry
	}
	return r
}
func inspectedSource(entry *outcome.Part, index int, inspected *inspection) {
	if entry.Kind != packet.PartSource || inspected == nil || index < 0 || index >= len(inspected.byLeaf) {
		return
	}
	occurrence := inspected.byLeaf[index].occurrence
	if occurrence == nil || occurrence.File == nil {
		return
	}
	if occurrence.Facts != nil && len(occurrence.Facts.PageSizes) > 0 {
		runs := append([]packet.PageSizeRun(nil), occurrence.Facts.PageSizes...)
		entry.PageSizes = &runs
	}
	facts := occurrence.File
	entry.SHA256 = fmt.Sprintf("%x", facts.SHA256)
	entry.Bytes = knownCount(facts.Bytes)
	if facts.Pages > 0 {
		entry.InputPages = knownCount(int64(facts.Pages))
	}
}
func declaredReportPart(leaf packet.Leaf) outcome.Part {
	p := outcome.Part{Item: leaf.Ref, Kind: leaf.Kind, Path: leaf.Source, Section: leaf.Owner}
	switch leaf.Kind {
	case packet.PartSource:
	case packet.PartPage, packet.PartDivider:
		p.Count = leaf.Count
	case packet.PartContents, packet.PartPadding:
	}
	return p
}
func reportSections(sections []packet.Section, resolved bool) []outcome.Section {
	resultSections := make([]outcome.Section, 0, len(sections))
	for _, section := range sections {
		s := outcome.Section{Item: section.Item, Title: section.Title}
		if section.Parent != "" {
			parent := section.Parent
			s.Parent = &parent
		}
		if resolved {
			s.TargetPage = knownCount(section.Target)
			s.OutputPages = &[2]int64{section.FirstPage, section.LastPage}
		}
		resultSections = append(resultSections, s)
	}
	return resultSections
}
