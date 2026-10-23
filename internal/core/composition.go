// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"fmt"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
)

type compositionOutcome struct {
	layout packet.Layout
	pages  map[int][]compose.Page
	result result.Result
	footer *compose.Footer
}

func composePacket(c context.Context, j packet.Plan, prepared preparationOutcome, r result.Result, operation Operation) compositionOutcome {
	fonts, facts, occurrences, domain := prepared.fonts, prepared.facts, prepared.occurrences, prepared.domain
	leaves, _ := j.Leaves(), j.Sections()
	for i, leaf := range leaves {
		if f, ok := facts[leaf.Source]; leaf.Kind == packet.PartSource && ok {
			r.Parts[i].SHA256 = fmt.Sprintf("%x", f.SHA256)
			r.Parts[i].Bytes = knownCount(f.Bytes)
			if f.Pages > 0 {
				r.Parts[i].InputPages = knownCount(int64(f.Pages))
			}
		}
	}
	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	var footer *compose.Footer
	if j.PageNumbers != nil {
		var err error
		footer, err = compose.NewFooter(fonts[packet.Font{}], *j.PageNumbers)
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, compose.Diagnostic(err, "/page_numbers"))
			return compositionOutcome{result: r}
		}
	}
	contents, ds := compose.PrepareContents(c, j, domain, fonts, footer)
	r.Diagnostics = append(r.Diagnostics, ds...)
	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	contentsCount := 0
	if contents != nil {
		contentsCount = contents.Pages()
	}
	layout, ds := packet.Resolve(j, domain, contentsCount)
	r.Diagnostics = append(r.Diagnostics, ds...)
	r.Diagnostics = append(r.Diagnostics, resolvePolicies(layout, facts, occurrences, j.FitTo, operation == Check)...)
	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	if j.PageNumbers != nil {
		for _, part := range layout.Parts {
			if part.Kind == packet.PartPadding {
				continue
			}
			var err error
			mappingIndex := 0
			for offset := range part.Pages {
				if err = c.Err(); err != nil {
					break
				}
				size := part.Size
				if j.FitTo != nil {
					size = *j.FitTo
				} else if part.Kind == packet.PartSource {
					number := part.FirstPage + int64(offset)
					for mappingIndex+1 < len(part.Mapping) && number >= part.Mapping[mappingIndex+1].OutputFirst {
						mappingIndex++
					}
					span := part.Mapping[mappingIndex]
					original := span.SourceFirst + int(number-span.OutputFirst)
					size = facts[part.Source].PageSizes[original]
				}
				if err = footer.Check(size, part.FirstPage+int64(offset)); err != nil {
					break
				}
			}
			if err != nil {
				d := compose.Diagnostic(err, "")
				d.Items = []string{part.Ref, "/page_numbers"}
				if part.Kind == packet.PartSource {
					d.File = part.Source
				}
				r.Diagnostics = append(r.Diagnostics, d)
			}
		}
	}

	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	r.Pages = &result.Pages{Source: knownCount(layout.SourcePages), Generated: knownCount(layout.GeneratedPages), Total: knownCount(layout.TotalPages)}
	r.Sections = reportSections(layout.Sections, true)
	declared := map[string]result.Part{}
	for _, entry := range r.Parts {
		declared[entry.Item] = entry
	}
	r.Parts = nil
	for _, part := range layout.Parts {
		entry, ok := declared[part.Ref]
		if !ok || part.Kind == packet.PartPadding {
			entry = declaredReportPart(part.Leaf)
		}
		entry.Kind = part.Kind
		entry.Pages = knownCount(int64(part.Pages))
		entry.OutputPages = &[2]int64{part.FirstPage, part.LastPage}
		entry.Section = part.Owner
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
		r.Parts = append(r.Parts, entry)
	}
	pages, texts, findings := compose.LayOut(c, layout, fonts)
	r.Diagnostics = append(r.Diagnostics, findings...)
	for i, text := range texts {
		r.Parts[i].Text = &result.Text{Font: text.Font, FontSize: text.FontSize, Bounds: text.Bounds, InkBounds: text.InkBounds, Overflow: text.Overflow}
	}
	if contents != nil {
		generated, findings := contents.Finish(layout)
		r.Diagnostics = append(r.Diagnostics, findings...)
		for i, part := range layout.Parts {
			if part.Kind == packet.PartContents {
				pages[i] = generated
			}
		}
	}
	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	return compositionOutcome{layout: layout, pages: pages, result: r, footer: footer}
}
