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

func layOut(c context.Context, layout packet.Layout, fonts map[string]*typeset.Font, r *result.Result) map[int][]pdf.Page {
	pages := map[int][]pdf.Page{}
	type cached struct {
		page pdf.Page
		err  error
	}
	cache := map[pageKey]cached{}
	var shaper typeset.Shaper
	for i, p := range layout.Parts {
		if p.Kind == "padding" {
			pages[i] = []pdf.Page{{Width: p.Size.Width, Height: p.Size.Height}}
			continue
		}
		if p.Page == nil {
			continue
		}
		if err := c.Err(); err != nil {
			r.Diagnostics = append(r.Diagnostics, diag.New("interrupted", err.Error()))
			break
		}
		style := p.Page
		key := pageKey{size: p.Size, text: style.Text, filled: style.Background != nil}
		if style.Background != nil {
			key.background = *style.Background
		}
		value, ok := cache[key]
		if !ok {
			value.page = pdf.Page{Width: p.Size.Width, Height: p.Size.Height, TextColor: pdf.Color{R: style.Text.Color.R, G: style.Text.Color.G, B: style.Text.Color.B}}
			if style.Background != nil {
				value.page.Background = &pdf.Color{R: style.Background.R, G: style.Background.G, B: style.Background.B}
			}
			if style.Text.Value != "" {
				width := style.Text.Width
				if _, explicit := style.Origins["width"]; !explicit {
					width = p.Size.Width - 72
				}
				if width < 1 {
					r.Diagnostics = append(r.Diagnostics, diag.New("text_overflow", "Default margins leave no usable text width; give an explicit width of at least 1 point or a wider page.", p.Pointer))
					continue
				}
				params := typeset.Params{Text: style.Text.Value, Size: style.Text.Size, PageWidth: p.Size.Width, PageHeight: p.Size.Height, OffsetX: style.Text.X, OffsetY: style.Text.Y, WrapWidth: width, LineSpacing: style.Text.Leading, Anchor: anchor(style.Text.Anchor), Align: alignment(style.Text.Align)}
				if style.Text.Overflow == "allow" {
					params.Overflow = typeset.OverflowAllow
				}
				value.page.Text, value.err = shaper.Place(fonts[style.Text.Font], params)
			}
			cache[key] = value
		}
		pages[i] = []pdf.Page{value.page}
		if placed := value.page.Text; placed != nil {
			text := &result.Text{Font: style.Text.Font, FontSize: placed.Size, Bounds: rect(placed.Bounds), Overflow: len(placed.Findings) > 0}
			if placed.InkBounds != nil {
				bounds := rect(*placed.InkBounds)
				text.InkBounds = &bounds
			}
			r.Parts[i].Text = text
			if len(placed.Findings) > 0 && value.err == nil {
				r.Diagnostics = append(r.Diagnostics, diag.New("text_overflow_allowed", "Measured text extends outside the page or text block.", p.Pointer))
			}
		}
		if value.err != nil {
			d := textDiagnostic(value.err, p.Pointer)
			if origin := style.Origins["width"]; origin != "" {
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

func anchor(value string) typeset.Anchor {
	switch value {
	case "top-left":
		return typeset.AnchorTopLeft
	case "top":
		return typeset.AnchorTopCenter
	case "top-right":
		return typeset.AnchorTopRight
	case "left":
		return typeset.AnchorMiddleLeft
	case "right":
		return typeset.AnchorMiddleRight
	case "bottom-left":
		return typeset.AnchorBottomLeft
	case "bottom":
		return typeset.AnchorBottomCenter
	case "bottom-right":
		return typeset.AnchorBottomRight
	default:
		return typeset.AnchorCenter
	}
}
func alignment(value string) typeset.Align {
	switch value {
	case "left":
		return typeset.AlignLeft
	case "right":
		return typeset.AlignRight
	case "justify":
		return typeset.AlignJustify
	default:
		return typeset.AlignCenter
	}
}

func resolvePolicies(layout packet.Layout, facts map[string]pdf.SourceFacts, occurrences map[string]pdf.OccurrenceFacts, fit *packet.Size, predict bool) []diag.Diagnostic {
	ds := pdf.CheckCompatibility(layout, facts, occurrences, fit).Diagnostics
	names := map[string]bool{}
	for i, p := range layout.Parts {
		if p.Kind != "source" {
			continue
		}
		f := facts[p.Source]
		add := func(code diag.Code, message string) {
			d := diag.New(code, message, p.Pointer)
			d.File = p.Source
			ds = append(ds, d)
		}
		if f.HasFields {
			if predict && i > 0 {
				add("form_fields_renamed", "Predicted: pdfcpu may namespace this later form, including distinct names; build records actual mappings.")
			}
		}
		collision := false
		for _, name := range occurrences[p.Pointer].DestNames {
			if names[name] {
				collision = true
			}
			names[name] = true
		}
		if predict && collision {
			add("destinations_renamed", "Predicted: colliding destination names will be renamed for affected occurrences.")
		}
	}
	return ds
}
