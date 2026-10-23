// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"math"
	"strconv"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type contentsRow struct {
	section         string
	page            int
	baseline, right float64
	bounds          typeset.Rect
}
type contentsLayout struct {
	pointer string
	pages   []pdf.Page
	rows    []contentsRow
	digits  *typeset.Digits
}

// prepareContents wraps titles with a conservative fixed digit column, before
// any parity or section target is known. There is no pagination fixed point.
func prepareContents(c context.Context, j packet.Plan, facts map[string]packet.SourceFacts, fonts map[packet.Font]*typeset.Font) (*contentsLayout, []diag.Diagnostic) {
	leaves, sections := j.Leaves(), j.Sections()
	var contents *packet.ContentsItem
	pointer := ""
	bound := int64(0)
	addBound := func(n int64) {
		if n > packet.MaxPageIndex-bound {
			bound = packet.MaxPageIndex
		} else {
			bound += n
		}
	}
	for _, leaf := range leaves {
		switch leaf.Kind {
		case packet.PartSource:
			addBound(int64(facts[leaf.Ref].Pages))
		case packet.PartContents:
			contents = leaf.Contents
			pointer = leaf.Ref
		case packet.PartPage, packet.PartDivider, packet.PartPadding:
			addBound(int64(leaf.Count))
		}
	}
	if contents == nil {
		return nil, nil
	}
	addBound(int64(len(sections)))
	addBound(int64(j.OddStarts()))
	fail := func(message string, item string) (*contentsLayout, []diag.Diagnostic) {
		return nil, []diag.Diagnostic{diag.New(diag.TextOverflow, message, pointer, item)}
	}
	if len(sections) == 0 {
		return fail("Contents requires at least one authored section.", pointer)
	}
	size := contents.Size
	if size == (packet.Size{}) {
		size, _ = packet.ParseSize("A4", false)
		if j.FitTo != nil {
			size = *j.FitTo
		}
	}
	font := fonts[packet.Font{}]
	digits, err := typeset.NewDigits(font, 12)
	if err != nil {
		return nil, []diag.Diagnostic{textDiagnostic(err, pointer)}
	}
	numberWidth := float64(len(strconv.FormatInt(max(bound, 1), 10))) * digits.CellWidth
	bottom := 36.0
	if j.PageNumbers != nil {
		footer, e := typeset.NewDigits(font, j.PageNumbers.Size)
		if e != nil {
			return nil, []diag.Diagnostic{textDiagnostic(e, "/page_numbers")}
		}
		protectedTop := j.PageNumbers.Margin - footer.InkBottom + footer.Top + 6
		scale, offset := 1.0, 0.0
		if j.FitTo != nil {
			scale = math.Min(j.FitTo.Width/size.Width, j.FitTo.Height/size.Height)
			offset = (j.FitTo.Height - size.Height*scale) / 2
		}
		bottom = math.Max(bottom, (protectedTop-offset)/scale)
	}
	layout := &contentsLayout{pointer: pointer, digits: digits}
	var shaper typeset.Shaper
	shape := func(text string, fontSize, width float64) (*typeset.Placed, error) {
		return shaper.Place(font, typeset.Params{Text: text, Size: fontSize, PageWidth: size.Width, PageHeight: size.Height, WrapWidth: width, LineSpacing: 1.2, Anchor: typeset.AnchorTopLeft, Align: typeset.AlignLeft, Overflow: typeset.OverflowAllow})
	}
	if size.Width-72 <= 0 {
		return fail("Contents margins leave no usable heading width; choose a larger canvas.", pointer)
	}
	heading, err := shape(contents.Title, 18, size.Width-72)
	if err != nil {
		return nil, []diag.Diagnostic{textDiagnostic(err, pointer)}
	}
	headingHeight := paintedBounds(heading).Height
	for _, finding := range heading.Findings {
		if finding.Kind == typeset.FindingWordTooWide {
			return fail("Contents heading does not fit its width; shorten it or choose a larger canvas.", pointer)
		}
	}
	headingCheck := clonePlaced(heading)
	movePlaced(headingCheck, 36, size.Height-36)
	if headingCheck.InkBounds != nil && headingCheck.InkBounds.X+headingCheck.InkBounds.Width > size.Width-36+1e-6 {
		return fail("Contents heading ink crosses its right margin; shorten it or choose a larger canvas.", pointer)
	}
	headingTop := size.Height - 36
	headingBottom := headingTop - headingHeight
	firstRowTop := headingBottom - 12
	if firstRowTop <= bottom {
		return fail("Contents heading and footer exclusion leave no room for entries; use a larger canvas or reduce page-number size/margin.", pointer)
	}
	addPage := func() {
		h := clonePlaced(heading)
		movePlaced(h, 36, headingTop)
		layout.pages = append(layout.pages, pdf.Page{Width: size.Width, Height: size.Height, Blocks: []pdf.TextBlock{{Text: h}}})
	}
	addPage()
	cursor := firstRowTop
	for _, section := range sections {
		if err := c.Err(); err != nil {
			return nil, []diag.Diagnostic{diag.New(diag.Interrupted, err.Error())}
		}
		left := 36 + 12*float64(section.Depth)
		right := size.Width - 36
		titleRight := right - numberWidth - 12
		width := titleRight - left
		if width < 1 {
			return fail("Contents indentation and number column leave no usable title width; reduce section nesting or choose a larger canvas.", section.Item)
		}
		title, e := shape(section.Title, 12, width)
		if e != nil {
			d := textDiagnostic(e, section.Item)
			d.Items = append(d.Items, pointer)
			return nil, []diag.Diagnostic{d}
		}
		for _, finding := range title.Findings {
			if finding.Kind == typeset.FindingWordTooWide {
				return fail("A contents title has a word wider than its column; shorten it or choose a larger canvas.", section.Item)
			}
		}
		height := paintedBounds(title).Height
		if height > firstRowTop-bottom+1e-6 {
			return fail("A whole contents row cannot fit beneath the heading and above the footer; shorten the title, use a larger canvas or reduce page-number size/margin.", section.Item)
		}
		if cursor-height < bottom-1e-6 {
			addPage()
			cursor = firstRowTop
		}
		movePlaced(title, left, cursor)
		if title.InkBounds != nil && title.InkBounds.X+title.InkBounds.Width > titleRight+1e-6 {
			return fail("A contents title's painted ink crosses its number column; shorten it or choose a larger canvas.", section.Item)
		}
		if len(title.Lines) == 0 {
			return fail("A contents title has no visible row.", section.Item)
		}
		title.Findings = nil
		page := len(layout.pages) - 1
		layout.pages[page].Blocks = append(layout.pages[page].Blocks, pdf.TextBlock{Text: title})
		baseline := title.Lines[0].Baseline
		rowBottom := math.Min(cursor-height, baseline+digits.Bottom)
		rowTop := math.Max(cursor, baseline+digits.Top)
		if rowBottom < bottom-1e-6 || rowTop > firstRowTop+1e-6 {
			return fail("Contents title and number bounds cannot fit in the available row; use a larger canvas or reduce page-number size/margin.", section.Item)
		}
		layout.rows = append(layout.rows, contentsRow{section: section.Item, page: page, baseline: baseline, right: right, bounds: typeset.Rect{X: left, Y: rowBottom, Width: right - left, Height: rowTop - rowBottom}})
		cursor = rowBottom - 6
	}
	return layout, nil
}

func (contents *contentsLayout) finish(layout packet.Layout, pages map[int][]pdf.Page) []diag.Diagnostic {
	targets := map[string]int64{}
	for _, section := range layout.Sections {
		targets[section.Item] = section.Target
	}
	for _, row := range contents.rows {
		target := targets[row.section]
		number, err := contents.digits.Place(strconv.FormatInt(target, 10), row.right, row.baseline)
		if err != nil {
			return []diag.Diagnostic{textDiagnostic(err, contents.pointer)}
		}
		contents.pages[row.page].Blocks = append(contents.pages[row.page].Blocks, pdf.TextBlock{Text: number})
		contents.pages[row.page].Links = append(contents.pages[row.page].Links, pdf.PageLink{Rect: row.bounds, TargetPage: target})
	}
	for i, part := range layout.Parts {
		if part.Kind == packet.PartContents && part.Ref == contents.pointer {
			pages[i] = contents.pages
			return nil
		}
	}
	return []diag.Diagnostic{diag.New(diag.AssemblyFailed, "Resolved contents item is missing.", contents.pointer)}
}

func paintedBounds(p *typeset.Placed) typeset.Rect {
	b := p.Bounds
	if p.InkBounds != nil {
		i := p.InkBounds
		right, top := math.Max(b.X+b.Width, i.X+i.Width), math.Max(b.Y+b.Height, i.Y+i.Height)
		b.X, b.Y = math.Min(b.X, i.X), math.Min(b.Y, i.Y)
		b.Width, b.Height = right-b.X, top-b.Y
	}
	return b
}
func movePlaced(p *typeset.Placed, left, top float64) {
	b := paintedBounds(p)
	dx, dy := left-b.X, top-b.Y-b.Height
	p.Bounds.X += dx
	p.Bounds.Y += dy
	if p.InkBounds != nil {
		p.InkBounds.X += dx
		p.InkBounds.Y += dy
	}
	for i := range p.Lines {
		p.Lines[i].X += dx
		p.Lines[i].Baseline += dy
	}
}
func clonePlaced(p *typeset.Placed) *typeset.Placed {
	cloned := *p
	cloned.Lines = append([]typeset.Line(nil), p.Lines...)
	if p.InkBounds != nil {
		b := *p.InkBounds
		cloned.InkBounds = &b
	}
	return &cloned
}
