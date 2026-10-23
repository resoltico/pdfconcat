// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

type pageKey struct {
	size       packet.Size
	text       packet.TextStyle
	background packet.Color
	filled     bool
}

func layOut(c context.Context, layout packet.Layout, fonts map[packet.Font]*typeset.Font, r *result.Result) map[int][]pdf.Page {
	pages := map[int][]pdf.Page{}
	type cached struct {
		page pdf.Page
		err  error
	}
	cache := map[pageKey]cached{}
	var shaper typeset.Shaper
	for i, p := range layout.Parts {
		if p.Kind == packet.PartPadding {
			pages[i] = []pdf.Page{{Width: p.Size.Width, Height: p.Size.Height}}
			continue
		}
		if p.Page == nil {
			continue
		}
		if err := c.Err(); err != nil {
			r.Diagnostics = append(r.Diagnostics, diag.New(diag.Interrupted, err.Error()))
			break
		}
		style := p.Page
		key := pageKey{size: p.Size, text: style.Text, filled: style.Background.Value != nil}
		if style.Background.Value != nil {
			key.background = *style.Background.Value
		}
		value, ok := cache[key]
		if !ok {
			value.page = pdf.Page{Width: p.Size.Width, Height: p.Size.Height, TextColor: pdf.Color{R: style.Text.Color.Value.R, G: style.Text.Color.Value.G, B: style.Text.Color.Value.B}}
			if style.Background.Value != nil {
				value.page.Background = &pdf.Color{R: style.Background.Value.R, G: style.Background.Value.G, B: style.Background.Value.B}
			}
			if style.Text.Value.Value != "" {
				width := style.Text.Width.Value
				if style.Text.Width.From == "" {
					width = p.Size.Width - 72
				}
				if width < 1 {
					r.Diagnostics = append(r.Diagnostics, diag.New(diag.TextOverflow, "Default margins leave no usable text width; give an explicit width of at least 1 point or a wider page.", p.Ref))
					continue
				}
				params := typeset.Params{Text: style.Text.Value.Value, Size: style.Text.Size.Value, PageWidth: p.Size.Width, PageHeight: p.Size.Height, OffsetX: style.Text.X.Value, OffsetY: style.Text.Y.Value, WrapWidth: width, LineSpacing: style.Text.Leading.Value, Anchor: anchor(style.Text.Anchor.Value), Align: alignment(style.Text.Align.Value)}
				if style.Text.Overflow.Value == packet.OverflowAllow {
					params.Overflow = typeset.OverflowAllow
				}
				value.page.Text, value.err = shaper.Place(fonts[style.Text.Font.Value], params)
			}
			cache[key] = value
		}
		pages[i] = []pdf.Page{value.page}
		if placed := value.page.Text; placed != nil {
			text := &result.Text{Font: style.Text.Font.Value.String(), FontSize: placed.Size, Bounds: rect(placed.Bounds), Overflow: len(placed.Findings) > 0}
			if placed.InkBounds != nil {
				bounds := rect(*placed.InkBounds)
				text.InkBounds = &bounds
			}
			r.Parts[i].Text = text
			if len(placed.Findings) > 0 && value.err == nil {
				r.Diagnostics = append(r.Diagnostics, diag.New(diag.TextOverflowAllowed, "Measured text extends outside the page or text block.", p.Ref))
			}
		}
		if value.err != nil {
			d := textDiagnostic(value.err, p.Ref)
			if origin := style.Text.Width.From; origin != "" {
				d.Message += " Width from " + origin + "."
			}
			r.Diagnostics = append(r.Diagnostics, d)
		}
	}
	return pages
}

func rect(r typeset.Rect) [4]float64 { return [4]float64{r.X, r.Y, r.X + r.Width, r.Y + r.Height} }
func placedTexts(pages map[int][]pdf.Page) []*typeset.Placed {
	var placed []*typeset.Placed
	for _, batch := range pages {
		for _, p := range batch {
			if p.Text != nil {
				placed = append(placed, p.Text)
			}
			for _, block := range p.Blocks {
				if block.Text != nil {
					placed = append(placed, block.Text)
				}
			}
		}
	}
	return placed
}

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

func resolvePolicies(layout packet.Layout, facts map[string]pdf.SourceFacts, occurrences map[string]pdf.OccurrenceFacts, fit *packet.Size, predict bool) []diag.Diagnostic {
	ds := pdf.CheckCompatibility(layout, facts, occurrences, fit).Diagnostics
	names := map[string]bool{}
	for i, p := range layout.Parts {
		if p.Kind != packet.PartSource {
			continue
		}
		f := facts[p.Source]
		add := func(code diag.Code, message string) {
			d := diag.New(code, message, p.Ref)
			d.File = p.Source
			ds = append(ds, d)
		}
		if f.HasFields {
			if predict && i > 0 {
				add(diag.FormFieldsRenamed, "Predicted: pdfcpu may namespace this later form, including distinct names; build records actual mappings.")
			}
		}
		collision := false
		for _, name := range occurrences[p.Ref].DestNames {
			if names[name] {
				collision = true
			}
			names[name] = true
		}
		if predict && collision {
			add(diag.DestinationsRenamed, "Predicted: colliding destination names will be renamed for affected occurrences.")
		}
	}
	return ds
}
