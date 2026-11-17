// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/compose"
	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

type composition struct {
	layout      *packet.Layout
	assembly    pdf.Assembly
	diagnostics []diag.Diagnostic
}

func composePacket(c context.Context, j packet.Plan, inspected *inspection, operation Operation) *composition {
	out := &composition{}
	shapes := inspected.sourceFacts()
	var numberLayout *compose.PageNumberLayout
	if j.PageNumbers() != nil {
		var err error
		numberLayout, err = compose.NewPageNumberLayout(inspected.fonts[packet.Font{}], *j.PageNumbers())
		if err != nil {
			out.diagnostics = append(out.diagnostics, compose.Diagnostic(err, "/page_numbers"))
			return out
		}
	}
	contents, ds := compose.PrepareContents(c, j, shapes, inspected.fonts, numberLayout)
	out.diagnostics = append(out.diagnostics, ds...)
	if hasErrors(out.diagnostics) {
		return out
	}
	count := 0
	if contents != nil {
		count = contents.Pages()
	}
	layout, ds := packet.Resolve(j, shapes, count)
	out.diagnostics = append(out.diagnostics, ds...)
	if hasErrors(out.diagnostics) {
		return out
	}
	out.assembly = pdf.Assembly{Numbering: layout.Numbering, Metadata: j.Metadata(), Timestamp: j.Timestamp(), Structure: j.Structure(), Parts: assemblyParts(layout, inspected), Sections: layout.Sections, FitTo: j.FitTo(), NumberLayout: numberLayout}
	out.diagnostics = append(out.diagnostics, resolvePolicies(out.assembly, operation == Check)...)
	if hasErrors(out.diagnostics) {
		return out
	}
	if j.PageNumbers() != nil {
		for _, part := range layout.Parts {
			if _, ok := packet.NumberAt(layout.Numbering, part.FirstPage); !ok {
				continue
			}
			var err error
			mappingIndex := 0
			for offset := range part.Pages {
				if err = c.Err(); err != nil {
					break
				}
				size := part.Size
				if j.FitTo() != nil {
					size = *j.FitTo()
				} else if part.Kind == packet.PartSource {
					number := part.FirstPage + int64(offset)
					for mappingIndex+1 < len(part.Mapping) && number >= part.Mapping[mappingIndex+1].OutputFirst {
						mappingIndex++
					}
					span := part.Mapping[mappingIndex]
					original := span.SourceFirst + int(number-span.OutputFirst)
					size = inspected.byLeaf[part.Index].occurrence.File.PageSizes[original]
				}
				if err = numberLayout.Check(size, numberForPage(layout.Numbering, part.FirstPage+int64(offset))); err != nil {
					break
				}
			}
			if err != nil {
				d := compose.Diagnostic(err, "")
				d.Items = []string{part.Ref, "/page_numbers"}
				if part.Kind == packet.PartSource {
					d.File = part.Source
				}
				out.diagnostics = append(out.diagnostics, d)
			}
		}
	}

	if hasErrors(out.diagnostics) {
		return out
	}
	out.layout = &layout
	prepared, ds := compose.LayOut(c, layout, inspected.fonts)
	out.diagnostics = append(out.diagnostics, ds...)
	if contents != nil {
		generated, ds := contents.Finish(layout)
		out.diagnostics = append(out.diagnostics, ds...)
		for i, part := range layout.Parts {
			if part.Kind == packet.PartContents {
				prepared[i].Pages = generated
			}
		}
	}
	out.diagnostics = append(out.diagnostics, bindComposition(out.assembly.Parts, prepared, !hasErrors(out.diagnostics))...)
	if !hasErrors(out.diagnostics) {
		out.assembly.Coverage = expectedStructureCoverage(out.assembly)
	}
	return out
}
func assemblyParts(layout packet.Layout, inspected *inspection) []pdf.Part {
	parts := make([]pdf.Part, len(layout.Parts))
	for i, part := range layout.Parts {
		parts[i] = pdf.Part{Part: part, Repeat: 1}
		switch part.Kind {
		case packet.PartSource:
			parts[i].Occurrence = inspected.byLeaf[part.Index].occurrence
		case packet.PartPage, packet.PartDivider:
			parts[i].Repeat = part.Count
		case packet.PartContents, packet.PartPadding:
		}
	}
	return parts
}

func numberForPage(spans []packet.NumberSpan, page int64) int64 {
	n, _ := packet.NumberAt(spans, page)
	return n
}

func expectedStructureCoverage(a pdf.Assembly) string {
	if a.Structure == packet.StructureDiscard {
		return "none"
	}
	covered, incomplete := false, false
	for _, part := range a.Parts {
		if part.Kind == packet.PartSource {
			value := part.Occurrence.Facts.StructureCoverage
			covered = covered || value != "none"
			incomplete = incomplete || part.Occurrence.Facts.HasUncoveredContent
		} else {
			for _, page := range part.Generated {
				for _, block := range page.Blocks {
					covered = covered || block.Text != nil && len(block.Text.Lines) > 0
				}
			}
		}
	}
	if !covered {
		return "none"
	}
	if incomplete {
		return "partial"
	}
	return "complete"
}

func bindComposition(parts []pdf.Part, prepared []compose.PreparedPart, complete bool) []diag.Diagnostic {
	failure := func() []diag.Diagnostic {
		return []diag.Diagnostic{diag.Report(diag.AssemblyFailed, diag.CompositionFactsDoNotMatchTheirOwningParts())}
	}
	if len(parts) != len(prepared) {
		return failure()
	}
	for index, part := range parts {
		fact := prepared[index]
		if fact.Owner != part.Identity() {
			return failure()
		}
		if complete && part.Page != nil && part.Page.Text.Value.Value != "" && fact.Text == nil {
			return failure()
		}
	}
	for index, fact := range prepared {
		parts[index].Generated = fact.Pages
		parts[index].Text = fact.Text
	}
	return nil
}
