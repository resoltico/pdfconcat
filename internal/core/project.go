// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"fmt"

	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

func project(req Request, a *admission, inspected *inspection, composed *composition, built *buildOutcome) Result {
	r := result.New(string(req.Operation))
	if a.roles.Output != "" {
		r.Output = &result.Output{Path: a.roles.Output}
	}
	if a.roles.Report != "" {
		r.Report = &result.Report{Path: a.roles.Report}
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
	r.FitTo = a.plan.FitTo
	r.PageNumbers = a.plan.PageNumbers
	r.Pages = &result.Pages{}
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
	r.Pages = &result.Pages{Source: knownCount(layout.SourcePages), Generated: knownCount(layout.GeneratedPages), Total: knownCount(layout.TotalPages)}
	r.Sections = reportSections(layout.Sections, true)
	r.Parts = make([]result.Part, len(layout.Parts))
	for i, part := range layout.Parts {
		entry := declaredReportPart(part.Leaf)
		inspectedSource(&entry, part.Index, inspected)
		entry.Pages = knownCount(int64(part.Pages))
		entry.OutputPages = &[2]int64{part.FirstPage, part.LastPage}
		entry.Reason = part.Reason
		entry.BeforeItem = part.BeforeItem
		if part.Kind == packet.PartSource {
			spans := make([]result.Span, 0, len(part.Mapping))
			for _, mapping := range part.Mapping {
				spans = append(spans, result.Span{Source: [2]int{mapping.SourceFirst, mapping.SourceLast}, Output: [2]int64{mapping.OutputFirst, mapping.OutputFirst + int64(mapping.SourceLast-mapping.SourceFirst)}})
			}
			entry.Spans = &spans
		} else {
			size := part.Size
			entry.Size = &size
		}
		if i < len(composed.texts) && composed.texts[i] != nil {
			text := composed.texts[i]
			entry.Text = &result.Text{Font: text.Font, FontSize: text.FontSize, Bounds: text.Bounds, InkBounds: text.InkBounds, Overflow: text.Overflow}
		}
		if built != nil && i < len(built.renames) && built.renames[i] != nil {
			entry.FieldsRenamed = built.renames[i]
		}
		r.Parts[i] = entry
	}
	return r
}
func inspectedSource(entry *result.Part, index int, inspected *inspection) {
	if entry.Kind != packet.PartSource || inspected == nil || index < 0 || index >= len(inspected.occurrences) {
		return
	}
	occurrence := inspected.occurrences[index]
	if occurrence == nil || occurrence.File == nil {
		return
	}
	facts := occurrence.File
	entry.SHA256 = fmt.Sprintf("%x", facts.SHA256)
	entry.Bytes = knownCount(facts.Bytes)
	if facts.Pages > 0 {
		entry.InputPages = knownCount(int64(facts.Pages))
	}
}
func declaredReportPart(leaf packet.Leaf) result.Part {
	p := result.Part{Item: leaf.Ref, Kind: leaf.Kind, Path: leaf.Source, Section: leaf.Owner}
	switch leaf.Kind {
	case packet.PartSource:
		p.FieldsRenamed = []packet.FieldRename{}
	case packet.PartPage, packet.PartDivider:
		p.Count = leaf.Count
	case packet.PartContents, packet.PartPadding:
	}
	return p
}
func reportSections(sections []packet.Section, resolved bool) []result.Section {
	resultSections := make([]result.Section, 0, len(sections))
	for _, section := range sections {
		s := result.Section{Item: section.Item, Title: section.Title}
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
