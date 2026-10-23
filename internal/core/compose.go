// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"
	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
)

type composition struct {
	layout      *packet.Layout
	assembly    pdf.Assembly
	texts       []*compose.TextFacts
	diagnostics []diag.Diagnostic
}

func composePacket(c context.Context, j packet.Plan, inspected *inspection, operation Operation) *composition {
	out := &composition{}
	var footer *compose.Footer
	if j.PageNumbers != nil {
		var err error
		footer, err = compose.NewFooter(inspected.fonts[packet.Font{}], *j.PageNumbers)
		if err != nil {
			out.diagnostics = append(out.diagnostics, compose.Diagnostic(err, "/page_numbers"))
			return out
		}
	}
	contents, ds := compose.PrepareContents(c, j, inspected.shapes, inspected.fonts, footer)
	out.diagnostics = append(out.diagnostics, ds...)
	if hasErrors(out.diagnostics) {
		return out
	}
	count := 0
	if contents != nil {
		count = contents.Pages()
	}
	layout, ds := packet.Resolve(j, inspected.shapes, count)
	out.diagnostics = append(out.diagnostics, ds...)
	if hasErrors(out.diagnostics) {
		return out
	}
	out.assembly = pdf.Assembly{Parts: assemblyParts(layout, inspected), Sections: layout.Sections, FitTo: j.FitTo, Footer: footer}
	out.diagnostics = append(out.diagnostics, resolvePolicies(out.assembly, operation == Check)...)
	if hasErrors(out.diagnostics) {
		return out
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
					size = inspected.occurrences[part.Index].File.PageSizes[original]
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
				out.diagnostics = append(out.diagnostics, d)
			}
		}
	}

	if hasErrors(out.diagnostics) {
		return out
	}
	out.layout = &layout
	pages, texts, ds := compose.LayOut(c, layout, inspected.fonts)
	out.texts = texts
	out.diagnostics = append(out.diagnostics, ds...)
	if contents != nil {
		generated, ds := contents.Finish(layout)
		out.diagnostics = append(out.diagnostics, ds...)
		for i, part := range layout.Parts {
			if part.Kind == packet.PartContents {
				pages[i] = generated
			}
		}
	}
	for i, batch := range pages {
		out.assembly.Parts[i].Generated = batch
	}
	return out
}
func assemblyParts(layout packet.Layout, inspected *inspection) []pdf.Part {
	parts := make([]pdf.Part, len(layout.Parts))
	for i, part := range layout.Parts {
		parts[i] = pdf.Part{Part: part, Repeat: 1}
		switch part.Kind {
		case packet.PartSource:
			parts[i].Occurrence = inspected.occurrences[part.Index]
		case packet.PartPage, packet.PartDivider:
			parts[i].Repeat = part.Count
		case packet.PartContents, packet.PartPadding:
		}
	}
	return parts
}
