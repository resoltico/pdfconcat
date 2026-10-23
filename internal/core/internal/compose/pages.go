// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package compose

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type pageKey struct {
	size       packet.Size
	text       packet.TextStyle
	background packet.Color
	filled     bool
}

// LayOut prepares page-item text and retains its measured facts.
func LayOut(c context.Context, layout packet.Layout, fonts map[packet.Font]*typeset.Font) ([][]Page, []*TextFacts, []diag.Diagnostic) {
	pages := make([][]Page, len(layout.Parts))
	facts := make([]*TextFacts, len(layout.Parts))
	var ds []diag.Diagnostic
	type cached struct {
		page Page
		err  error
	}
	cache := map[pageKey]cached{}
	var shaper typeset.Shaper
	for i, p := range layout.Parts {
		if p.Kind == packet.PartPadding {
			pages[i] = []Page{{Width: p.Size.Width, Height: p.Size.Height}}
			continue
		}
		if p.Page == nil {
			continue
		}
		if err := c.Err(); err != nil {
			ds = append(ds, diag.New(diag.Interrupted, err.Error()))
			break
		}
		style := p.Page
		key := pageKey{size: p.Size, text: style.Text, filled: style.Background.Value != nil}
		if style.Background.Value != nil {
			key.background = *style.Background.Value
		}
		value, ok := cache[key]
		if !ok {
			value.page = Page{Width: p.Size.Width, Height: p.Size.Height, Blocks: []TextBlock{{Color: packet.Color{R: style.Text.Color.Value.R, G: style.Text.Color.Value.G, B: style.Text.Color.Value.B}}}}
			if style.Background.Value != nil {
				value.page.Background = &packet.Color{R: style.Background.Value.R, G: style.Background.Value.G, B: style.Background.Value.B}
			}
			if style.Text.Value.Value != "" {
				width := style.Text.Width.Value
				if style.Text.Width.From == "" {
					width = p.Size.Width - 72
				}
				if width < 1 {
					ds = append(ds, diag.New(diag.TextOverflow, "Default margins leave no usable text width; give an explicit width of at least 1 point or a wider page.", p.Ref))
					continue
				}
				params := typeset.Params{Text: style.Text.Value.Value, Size: style.Text.Size.Value, PageWidth: p.Size.Width, PageHeight: p.Size.Height, OffsetX: style.Text.X.Value, OffsetY: style.Text.Y.Value, WrapWidth: width, LineSpacing: style.Text.Leading.Value, Anchor: anchor(style.Text.Anchor.Value), Align: alignment(style.Text.Align.Value)}
				if style.Text.Overflow.Value == packet.OverflowAllow {
					params.Overflow = typeset.OverflowAllow
				}
				value.page.Blocks[0].Text, value.err = shaper.Place(fonts[style.Text.Font.Value], params)
			}
			cache[key] = value
		}
		pages[i] = []Page{value.page}
		if placed := value.page.Blocks[0].Text; placed != nil {
			text := &TextFacts{Font: style.Text.Font.Value.String(), FontSize: placed.Size, Bounds: rect(placed.Bounds), Overflow: len(placed.Findings) > 0}
			if placed.InkBounds != nil {
				bounds := rect(*placed.InkBounds)
				text.InkBounds = &bounds
			}
			facts[i] = text
			if len(placed.Findings) > 0 && value.err == nil {
				ds = append(ds, diag.New(diag.TextOverflowAllowed, "Measured text extends outside the page or text block.", p.Ref))
			}
		}
		if value.err != nil {
			d := Diagnostic(value.err, p.Ref)
			if origin := style.Text.Width.From; origin != "" {
				d.Message += " Width from " + origin + "."
			}
			ds = append(ds, d)
		}
	}
	return pages, facts, ds
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
