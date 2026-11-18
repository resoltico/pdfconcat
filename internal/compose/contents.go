// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"context"
	"math"
	"strconv"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type contentsRow struct {
	label           string
	titleBlock      int
	section         string
	page            int
	baseline, right float64
	bounds          typeset.Rect
}

// Contents retains paginated rows until their physical targets are resolved.
type Contents struct {
	pointer string
	pages   []Page
	rows    []contentsRow
	digits  *typeset.Digits
	dash    *typeset.Placed
}

// PrepareContents wraps titles with a conservative fixed digit column, before
// any parity or section target is known. There is no pagination fixed point.
func PrepareContents(c context.Context, j packet.Plan, facts []packet.SourceFacts, fonts map[packet.Font]*typeset.Font, numberLayout *PageNumberLayout) (*Contents, []diag.Diagnostic) {
	leaves, sections := j.Leaves(), j.Sections()
	var contents *packet.ContentsItem
	pointer := ""
	bound := int64(0)
	anyEligible := false
	addBound := func(n int64) {
		if n > packet.MaxPageIndex-bound {
			bound = packet.MaxPageIndex
		} else {
			bound += n
		}
	}
	for _, leaf := range leaves {
		if j.PageNumbers() != nil && j.PageNumbers().Eligible(leaf.Kind, leaf.NumberingEnabled) {
			anyEligible = true
		}
		switch leaf.Kind {
		case packet.PartSource:
			if leaf.Index < len(facts) {
				addBound(int64(facts[leaf.Index].Pages))
			}
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
	if j.PageNumbers() != nil {
		addBound(j.PageNumbers().First() - 1)
	}
	fail := func(message diag.Explanation, item string) (*Contents, []diag.Diagnostic) {
		return nil, []diag.Diagnostic{diag.Report(diag.TextOverflow, message, pointer, item)}
	}
	if len(sections) == 0 {
		return fail(diag.ContentsRequiresAtLeastOneAuthoredSection(), pointer)
	}
	size := j.ContentsCanvas(contents.Size)
	font := fonts[j.PageDefaults().Text.Font.Value]
	digits, err := typeset.NewDigits(font, 12)
	if err != nil {
		return nil, []diag.Diagnostic{Diagnostic(err, pointer)}
	}
	numberWidth := float64(len(strconv.FormatInt(max(bound, 1), 10))) * digits.CellWidth
	if j.PageNumbers() != nil && !anyEligible {
		numberWidth = 0
	}
	bottom, top := 36.0, size.Height-36
	eligible := false
	for _, leaf := range leaves {
		if leaf.Kind == packet.PartContents && j.PageNumbers() != nil {
			eligible = j.PageNumbers().Eligible(leaf.Kind, leaf.NumberingEnabled)
		}
	}
	if numberLayout != nil && eligible {
		fit, err := packet.DecideFit(size, j.Fitting())
		if err != nil {
			return nil, []diag.Diagnostic{Diagnostic(err, pointer)}
		}
		lo, hi := numberLayout.Available(size, fit.Output)
		bottom = math.Max(bottom, lo)
		top = math.Min(top, hi)
	}

	layout := &Contents{pointer: pointer, digits: digits}
	var shaper typeset.Shaper
	shape := func(text string, fontSize, width float64) (*typeset.Placed, error) {
		return shaper.Place(font, typeset.Params{Language: typeset.ShapingLanguage(j.Language()), Text: text, Size: fontSize, PageWidth: size.Width, PageHeight: size.Height, WrapWidth: width, LineSpacing: 1.2, Anchor: typeset.AnchorTopLeft, Align: typeset.AlignLeft, Overflow: typeset.OverflowAllow})
	}
	if size.Width-72 <= 0 {
		return fail(diag.ContentsMarginsLeaveNoUsableHeadingWidthChooseALarger(), pointer)
	}
	dash, err := shape("—", 12, 0)
	if err != nil {
		return nil, []diag.Diagnostic{Diagnostic(err, pointer)}
	}
	layout.dash = dash
	numberWidth = math.Max(numberWidth, paintedBounds(dash).Width)
	heading, err := shape(contents.Title, 18, size.Width-72)
	if err != nil {
		return nil, []diag.Diagnostic{Diagnostic(err, pointer)}
	}
	headingHeight := paintedBounds(heading).Height
	for _, finding := range heading.Findings {
		if finding.Kind == typeset.FindingWordTooWide {
			return fail(diag.ContentsHeadingDoesNotFitItsWidthShortenItOr(), pointer)
		}
	}
	headingCheck := clonePlaced(heading)
	movePlaced(headingCheck, 36, size.Height-36)
	if headingCheck.InkBounds != nil && headingCheck.InkBounds.X+headingCheck.InkBounds.Width > size.Width-36+1e-6 {
		return fail(diag.ContentsHeadingInkCrossesItsRightMarginShortenItOr(), pointer)
	}
	headingTop := top
	headingBottom := headingTop - headingHeight
	firstRowTop := headingBottom - 12
	if firstRowTop <= bottom {
		return fail(diag.ContentsHeadingAndPageNumberExclusionLeaveNoRoomFor(), pointer)
	}
	addPage := func() {
		h := clonePlaced(heading)
		movePlaced(h, 36, headingTop)
		layout.pages = append(layout.pages, Page{Width: size.Width, Height: size.Height, Blocks: []TextBlock{{Role: RoleHeading, Text: h}}})
	}
	addPage()
	cursor := firstRowTop
	for _, section := range sections {
		if err := c.Err(); err != nil {
			return nil, []diag.Diagnostic{diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error()))}
		}
		left := 36 + 12*float64(section.Depth)
		right := size.Width - 36
		titleRight := right - numberWidth - 12
		width := titleRight - left
		if width < 1 {
			return fail(diag.ContentsIndentationAndNumberColumnLeaveNoUsableTitleWidth(), section.Item)
		}
		title, e := shape(section.Title, 12, width)
		if e != nil {
			d := Diagnostic(e, section.Item)
			d.Items = append(d.Items, pointer)
			return nil, []diag.Diagnostic{d}
		}
		for _, finding := range title.Findings {
			if finding.Kind == typeset.FindingWordTooWide {
				return fail(diag.AContentsTitleHasAWordWiderThanItsColumn(), section.Item)
			}
		}
		height := paintedBounds(title).Height
		if height > firstRowTop-bottom+1e-6 {
			return fail(diag.AWholeContentsRowCannotFitBeneathTheHeadingAnd(), section.Item)
		}
		if cursor-height < bottom-1e-6 {
			addPage()
			cursor = firstRowTop
		}
		movePlaced(title, left, cursor)
		if title.InkBounds != nil && title.InkBounds.X+title.InkBounds.Width > titleRight+1e-6 {
			return fail(diag.AContentsTitleSPaintedInkCrossesItsNumberColumn(), section.Item)
		}
		if len(title.Lines) == 0 {
			return fail(diag.AContentsTitleHasNoVisibleRow(), section.Item)
		}
		title.Findings = nil
		page := len(layout.pages) - 1
		layout.pages[page].Blocks = append(layout.pages[page].Blocks, TextBlock{Role: RoleContents, Text: title})
		baseline := title.Lines[0].Baseline
		rowBottom := math.Min(cursor-height, baseline+digits.Bottom)
		rowTop := math.Max(cursor, baseline+digits.Top)
		if rowBottom < bottom-1e-6 || rowTop > firstRowTop+1e-6 {
			return fail(diag.ContentsTitleAndNumberBoundsCannotFitInTheAvailable(), section.Item)
		}
		layout.rows = append(layout.rows, contentsRow{label: section.Title, titleBlock: len(layout.pages[page].Blocks) - 1, section: section.Item, page: page, baseline: baseline, right: right, bounds: typeset.Rect{X: left, Y: rowBottom, Width: right - left, Height: rowTop - rowBottom}})
		cursor = rowBottom - 6
	}
	return layout, nil
}

// Finish fills in target numbers and links without changing pagination.
func (contents *Contents) Finish(layout packet.Layout) ([]Page, []diag.Diagnostic) {
	targets := map[string]int64{}
	for _, section := range layout.Sections {
		targets[section.Item] = section.Target
	}
	pages := make([]Page, len(contents.pages))
	for i, page := range contents.pages {
		pages[i] = page
		pages[i].Blocks = append([]TextBlock(nil), page.Blocks[:1]...)
		pages[i].Links = nil
	}
	for _, row := range contents.rows {
		target := targets[row.section]
		label := layout.LabelAt(target)
		var number *typeset.Placed
		var err error
		if label == "—" {
			number = clonePlaced(contents.dash)
			b := paintedBounds(number)
			movePlaced(number, row.right-b.Width, row.baseline-number.Lines[0].Baseline+b.Y+b.Height)
		} else {
			number, err = contents.digits.Place(label, row.right, row.baseline)
		}
		if err != nil {
			return nil, []diag.Diagnostic{Diagnostic(err, contents.pointer)}
		}
		link := len(pages[row.page].Links)
		title := contents.pages[row.page].Blocks[row.titleBlock]
		title.Link = &link
		// Keep each link's marked content contiguous as well as logically grouped.
		pages[row.page].Blocks = append(pages[row.page].Blocks, title, TextBlock{Role: RoleContents, Link: &link, Text: number})
		pages[row.page].Links = append(pages[row.page].Links, Link{Label: row.label, Rect: row.bounds, TargetPage: target})
	}
	for _, part := range layout.Parts {
		if part.Kind == packet.PartContents && part.Ref == contents.pointer {
			return pages, nil
		}
	}
	return nil, []diag.Diagnostic{diag.Report(diag.AssemblyFailed, diag.ResolvedContentsItemIsMissing(), contents.pointer)}
}

// Pages returns the prepared page count.
func (contents *Contents) Pages() int { return len(contents.pages) }

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

func shiftPlaced(p *typeset.Placed, dx, dy float64) {
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
