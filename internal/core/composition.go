// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"fmt"

	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/result"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type compositionOutcome struct {
	layout job.Layout
	pages  map[int][]pdf.Page
	result result.Result
}

func compose(c context.Context, j job.Job, prepared preparationOutcome, r result.Result, operation Operation) compositionOutcome {
	fonts, facts, occurrences, domain := prepared.fonts, prepared.facts, prepared.occurrences, prepared.domain
	leaves, _ := job.Declarations(j)
	for i, leaf := range leaves {
		if f, ok := facts[leaf.Source]; leaf.Kind == "source" && ok {
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
	contents, ds := prepareContents(c, j, domain, fonts)
	r.Diagnostics = append(r.Diagnostics, ds...)
	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	contentsCount := 0
	if contents != nil {
		contentsCount = len(contents.pages)
	}
	layout, ds := job.Resolve(j, domain, contentsCount)
	r.Diagnostics = append(r.Diagnostics, ds...)
	r.Diagnostics = append(r.Diagnostics, resolvePolicies(layout, facts, occurrences, j.FitTo, operation == Check)...)
	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	if j.PageNumbers != nil {
		digits, e := typeset.NewDigits(fonts["default"], j.PageNumbers.Size)
		if e != nil {
			r.Diagnostics = append(r.Diagnostics, textDiagnostic(e, "/page_numbers"))
			return compositionOutcome{result: r}
		}
		for _, part := range layout.Parts {
			if part.Kind == "padding" {
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
				} else if part.Kind == "source" {
					number := part.FirstPage + int64(offset)
					for mappingIndex+1 < len(part.Mapping) && number >= part.Mapping[mappingIndex+1].OutputFirst {
						mappingIndex++
					}
					span := part.Mapping[mappingIndex]
					original := span.SourceFirst + int(number-span.OutputFirst)
					size = facts[part.Source].PageSizes[original]
				}
				if err = pdf.CheckNumberPlacement(digits, size, part.FirstPage+int64(offset), j.PageNumbers.Margin); err != nil {
					break
				}
			}
			if err != nil {
				d := pdf.Diagnostic(err)
				d.Items = []string{part.Pointer, "/page_numbers"}
				if part.Kind == "source" {
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
		entry, ok := declared[part.Pointer]
		if !ok || part.Kind == "padding" {
			entry = declaredReportPart(part.Leaf)
		}
		entry.Kind = part.Kind
		entry.Pages = knownCount(int64(part.Pages))
		entry.OutputPages = &[2]int64{part.FirstPage, part.LastPage}
		entry.Section = part.Owner
		entry.Reason = part.Reason
		entry.BeforeItem = part.BeforeItem
		if part.Kind == "source" {
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
	pages := layOut(c, layout, fonts, &r)
	if contents != nil {
		r.Diagnostics = append(r.Diagnostics, contents.finish(layout, pages)...)
	}
	if hasErrors(r.Diagnostics) {
		return compositionOutcome{result: r}
	}
	return compositionOutcome{layout: layout, pages: pages, result: r}
}
