// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"strings"

	"github.com/resoltico/pdfconcat/internal/assembly"
	"github.com/resoltico/pdfconcat/internal/report"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

// describe adds the sources, parts and styles known so far to the report. A layout that was never
// resolved still yields every part, with the pages it can know and no range, so an agent can retrieve
// the failing part by its ID. It runs once.
func (p *pipeline) describe() {
	if p.described || p.flat == nil {
		return
	}

	p.described = true

	sources := make([]int, len(p.flat.Files))
	for index := range p.flat.Files {
		sources[index] = p.builder.Source(p.sourceEntry(index))
	}

	styles := map[[2]int]int{}
	fonts := map[string]int{}

	for index := range p.flat.Contributions {
		contribution := &p.flat.Contributions[index]
		location := assembly.Locate(p.flat.Source, contribution.Origin, "")
		part := report.Part{ID: location.Pointer, Origin: report.OriginOf(location)}

		if contribution.Kind == assembly.ItemPDF {
			part.Kind = report.PartPDF
			part.Source = &sources[contribution.File]

			if captured := &p.files[contribution.File]; captured.captured.Path != "" {
				pages := int64(captured.info.Pages)
				part.Pages = &pages
			}
		} else {
			count := contribution.Count
			part.Kind, part.Pages = report.PartBlank, &count
		}

		if p.layout != nil {
			p.place(&part, index, styles, fonts)
		}

		p.builder.AddPart(part)
	}
}

// sourceEntry is the report entry of one distinct source; its identity is known once it was captured.
func (p *pipeline) sourceEntry(index int) report.Source {
	entry := report.Source{Path: p.flat.Files[index].Path}

	if captured := &p.files[index].captured; captured.Path != "" {
		entry.Digest = captured.Digest()
		size := captured.Size
		entry.Bytes = &size
	}

	return entry
}

// place adds the output range and, for a blank, the resolved style of one contribution.
func (p *pipeline) place(part *report.Part, index int, styles map[[2]int]int, fonts map[string]int) {
	placement := &p.layout.Placements[index]
	part.Range = &report.PageRange{Start: placement.Range.Start, End: placement.Range.End}

	if placement.Spec < 0 {
		return
	}

	// A style is the distinct generated page together with how its size was obtained.
	key := [2]int{placement.Spec, int(placement.Size)}

	style, found := styles[key]
	if !found {
		entry := p.styleOf(placement, fonts)
		style = p.builder.Style(&entry)
		styles[key] = style
	}

	part.Style = &style
}

// styleOf is the report style of a generated page as placed.
func (p *pipeline) styleOf(placement *assembly.Placement, fonts map[string]int) report.Style {
	spec := &p.layout.Specs[placement.Spec].Spec
	style := report.Style{
		Background: strings.ToLower(spec.Background.String()),
		Size: report.PageSize{
			Origin: sizeOrigin(placement.Size), Width: float64(spec.Dim.Width), Height: float64(spec.Dim.Height),
		},
	}

	if spec.Text.Value == "" {
		return style
	}

	text := &spec.Text

	style.Text = &report.Text{
		Font:      p.fontEntry(text.Font.File, fonts),
		Value:     text.Value,
		Color:     strings.ToLower(text.Color.String()),
		Anchor:    text.Anchor.String(),
		Align:     text.Align.String(),
		Overflow:  text.Overflow.String(),
		Size:      float64(text.Size),
		X:         float64(text.X),
		Y:         float64(text.Y),
		Width:     float64(text.Width),
		Leading:   text.Leading,
		Bounds:    p.boundsOf(placement.Spec),
		InkBounds: p.inkBoundsOf(placement.Spec),
	}
	if placement.Spec < len(p.placed) && p.placed[placement.Spec] != nil {
		for _, finding := range p.placed[placement.Spec].Findings {
			style.Text.Findings = append(style.Text.Findings, report.TextFinding{
				Kind: string(finding.Kind), Detail: finding.Detail, Line: finding.Line,
			})
		}
	}

	return style
}

// boundsOf is the computed text block of a generated page; it is empty when the text was not placed.
func (p *pipeline) boundsOf(spec int) *report.Rect {
	if spec >= len(p.placed) || p.placed[spec] == nil {
		return nil
	}

	bounds := p.placed[spec].Bounds

	return &report.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height}
}

// fontEntry interns the font of a style in the report, once per path.
func (p *pipeline) fontEntry(path string, fonts map[string]int) int {
	if index, found := fonts[path]; found {
		return index
	}

	font, _ := p.fonts.lookup(path)
	entry := report.Font{Digest: report.HexDigest(font.Identity()), Name: fontName(font, path), File: path}
	index := p.builder.Font(entry)
	fonts[path] = index

	return index
}

// fontName is the name a report shows for a font: the built-in font's name, or the PostScript name.
func fontName(font *typeset.Font, path string) string {
	if path == "" {
		return typeset.DefaultFontName
	}

	return font.PostScriptName()
}

// sizeOrigin converts how a page got its size.
func sizeOrigin(size assembly.SizeSource) report.SizeOrigin {
	if size == assembly.SizeFromPrecedingLast {
		return report.SizePrecedingSource
	}

	if size == assembly.SizeFromFollowingFirst {
		return report.SizeFollowingSource
	}

	return report.SizeExplicit
}

func (p *pipeline) inkBoundsOf(spec int) *report.Rect {
	if spec >= len(p.placed) || p.placed[spec] == nil || p.placed[spec].InkBounds == nil {
		return nil
	}

	bounds := p.placed[spec].InkBounds

	return &report.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height}
}
