// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"context"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

type pageKey struct {
	language    string
	bottom, top float64
	kind        packet.PartKind
	size        packet.Size
	text        packet.TextStyle
	background  packet.Color
	filled      bool
}

// PreparedPart binds measured text and generated pages to their owning span.
type PreparedPart struct {
	Owner packet.PartIdentity
	Pages []Page
	Text  *TextFacts
}

// LayOut prepares page-item text and retains its measured facts.
func LayOut(c context.Context, layout packet.Layout, fonts map[packet.Font]*typeset.Font, numberLayout *PageNumberLayout) ([]PreparedPart, []diag.Diagnostic) {
	prepared := make([]PreparedPart, len(layout.Parts))
	for index, part := range layout.Parts {
		prepared[index].Owner = part.Identity()
	}
	var ds []diag.Diagnostic
	type cached struct {
		page Page
		err  error
	}
	cache := map[pageKey]cached{}
	var shaper typeset.Shaper
	for i, p := range layout.Parts {
		if p.Kind == packet.PartPadding {
			prepared[i].Pages = []Page{{Width: p.Size.Width, Height: p.Size.Height}}
			continue
		}
		if p.Page == nil {
			continue
		}
		if err := c.Err(); err != nil {
			ds = append(ds, diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error())))
			break
		}
		style := p.Page
		bottom, top := 0.0, p.Size.Height
		if style.Text.Value.Value != "" && numberLayout != nil {
			if _, eligible := packet.NumberAt(layout.Numbering, p.FirstPage); eligible {
				bottom, top = numberLayout.Available(p.Size, p.OutputSize)
			}
		}
		if top-bottom < 1 {
			ds = append(ds, diag.Report(diag.TextOverflow, diag.PageNumberExclusionLeavesNoUsableTextHeight(), p.Ref))
			continue
		}
		key := pageKey{language: layout.Language, bottom: bottom, top: top, kind: p.Kind, size: p.Size, text: style.Text, filled: style.Background.Value != nil}
		if style.Background.Value != nil {
			key.background = *style.Background.Value
		}
		value, ok := cache[key]
		if !ok {
			role := RoleParagraph
			if p.Kind == packet.PartDivider {
				role = RoleHeading
			}
			value.page = Page{Width: p.Size.Width, Height: p.Size.Height, Blocks: []TextBlock{{Role: role, Color: packet.Color{R: style.Text.Color.Value.R, G: style.Text.Color.Value.G, B: style.Text.Color.Value.B}}}}
			if style.Background.Value != nil {
				value.page.Background = &packet.Color{R: style.Background.Value.R, G: style.Background.Value.G, B: style.Background.Value.B}
			}
			if style.Text.Value.Value != "" {
				width := style.Text.Width.Value
				if !style.Text.Width.Present {
					width = p.Size.Width - 72
				}
				if width < 1 {
					d := diag.Report(diag.TextOverflow, diag.DefaultMarginsLeaveNoUsableTextWidthGiveAnExplicit(), p.Ref)
					if style.Size.Value == p.Size {
						d.AuthoredRef = style.Size.From
					}
					ds = append(ds, d)
					continue
				}
				params := typeset.Params{Language: typeset.ShapingLanguage(layout.Language), Text: style.Text.Value.Value, Size: style.Text.Size.Value, PageWidth: p.Size.Width, PageHeight: top - bottom, OffsetX: style.Text.X.Value, OffsetY: style.Text.Y.Value, WrapWidth: width, LineSpacing: style.Text.Leading.Value, Anchor: anchor(style.Text.Anchor.Value), Align: alignment(style.Text.Align.Value)}
				if style.Text.Overflow.Value == packet.OverflowAllow {
					params.Overflow = typeset.OverflowAllow
				}
				if style.Text.Wrap.Value == packet.WrapEmergency {
					params.Wrap = typeset.WrapEmergency
				}
				value.page.Blocks[0].Text, value.err = shaper.Place(fonts[style.Text.Font.Value], params)
				if value.page.Blocks[0].Text != nil {
					shiftPlaced(value.page.Blocks[0].Text, 0, bottom)
				}
			}
			cache[key] = value
		}
		prepared[i].Pages = []Page{value.page}
		if placed := value.page.Blocks[0].Text; placed != nil {
			text := &TextFacts{Font: style.Text.Font.Value.String(), FontSize: placed.Size, Bounds: rect(placed.Bounds), Overflow: len(placed.Findings) > 0}
			if placed.InkBounds != nil {
				bounds := rect(*placed.InkBounds)
				text.InkBounds = &bounds
			}
			prepared[i].Text = text
			if len(placed.Findings) > 0 && value.err == nil {
				d := diag.Report(diag.TextOverflowAllowed, allowedTextOverflow(placed, p.Size, bottom, top), p.Ref)
				for _, finding := range placed.Findings {
					if finding.Kind == typeset.FindingWordTooWide {
						d.AuthoredRef = style.Text.Width.From
						break
					}
				}
				ds = append(ds, d)
			}
		}
		if value.err != nil {
			d := diagnosticWidth(value.err, p.Ref, style.Text.Width.From, 0)
			ds = append(ds, d)
		}
	}
	return prepared, ds
}

func allowedTextOverflow(placed *typeset.Placed, sheet packet.Size, bottom, top float64) diag.Explanation {
	if ink := placed.InkBounds; ink != nil {
		if ink.X < 0 || ink.X+ink.Width > sheet.Width {
			return diag.AllowedGlyphInkOutsideSheet("x", ink.X, ink.X+ink.Width, sheet.Width)
		}
		if ink.Y < 0 || ink.Y+ink.Height > sheet.Height {
			return diag.AllowedGlyphInkOutsideSheet("y", ink.Y, ink.Y+ink.Height, sheet.Height)
		}
		if ink.Y < bottom || ink.Y+ink.Height > top {
			return diag.AllowedTextOutsideArea("Glyph ink", "y", ink.Y, ink.Y+ink.Height, bottom, top)
		}
	}
	for _, finding := range placed.Findings {
		switch finding.Kind {
		case typeset.FindingOutsidePageHorizontal:
			r := placed.Bounds
			return diag.AllowedTextOutsideArea("Text layout", "x", r.X, r.X+r.Width, 0, sheet.Width)
		case typeset.FindingOutsidePageVertical:
			r := placed.Bounds
			return diag.AllowedTextOutsideArea("Text layout", "y", r.Y, r.Y+r.Height, bottom, top)
		case typeset.FindingWordTooWide:
		}
	}
	var measurement string
	if len(placed.Findings) != 0 {
		measurement = placed.Findings[0].Detail
	}
	return diag.AllowedTextExceedsWrapWidth(measurement)
}

func rect(r typeset.Rect) [4]float64 { return [4]float64{r.X, r.Y, r.X + r.Width, r.Y + r.Height} }

func anchor(value packet.Anchor) typeset.Anchor {
	switch value {
	case packet.AnchorTopLeft:
		return typeset.AnchorTopLeft
	case packet.AnchorTop:
		return typeset.AnchorTopCenter
	case packet.AnchorTopRight:
		return typeset.AnchorTopRight
	case packet.AnchorLeft:
		return typeset.AnchorMiddleLeft
	case packet.AnchorRight:
		return typeset.AnchorMiddleRight
	case packet.AnchorBottomLeft:
		return typeset.AnchorBottomLeft
	case packet.AnchorBottom:
		return typeset.AnchorBottomCenter
	case packet.AnchorBottomRight:
		return typeset.AnchorBottomRight
	case packet.AnchorCenter:
		return typeset.AnchorCenter
	}
	return typeset.AnchorCenter
}
func alignment(value packet.Align) typeset.Align {
	switch value {
	case packet.AlignLeft:
		return typeset.AlignLeft
	case packet.AlignRight:
		return typeset.AlignRight
	case packet.AlignJustify:
		return typeset.AlignJustify
	case packet.AlignCenter:
		return typeset.AlignCenter
	}
	return typeset.AlignCenter
}
