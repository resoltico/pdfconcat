// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/resoltico/pdfconcat/internal/diag"
	"github.com/resoltico/pdfconcat/internal/inputfile"
	"github.com/resoltico/pdfconcat/internal/job"
	"github.com/resoltico/pdfconcat/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/result"
	"github.com/resoltico/pdfconcat/internal/typeset"
)

func loadFonts(c context.Context, j job.Job, r *result.Result) map[string]*typeset.Font {
	fonts := map[string]*typeset.Font{}
	var fontOrder []string
	fontItems := map[string][]string{}
	type textKey struct{ font, value string }
	var textOrder []textKey
	textItems := map[textKey][]string{}
	for _, p := range j.Parts {
		if p.Page == nil || p.Page.Text.Value == "" {
			continue
		}
		name := p.Page.Text.Font
		if _, ok := fontItems[name]; !ok {
			fontOrder = append(fontOrder, name)
		}
		fontItems[name] = append(fontItems[name], p.Pointer)
		key := textKey{font: name, value: p.Page.Text.Value}
		if _, ok := textItems[key]; !ok {
			textOrder = append(textOrder, key)
		}
		textItems[key] = append(textItems[key], p.Pointer)
	}
	for _, name := range fontOrder {
		if c.Err() != nil {
			noteCancellation(c, r)
			return fonts
		}
		var font *typeset.Font
		var err error
		if name == "default" {
			font, err = typeset.LoadDefaultFont()
		} else {
			var data []byte
			data, err = inputfile.Read(c, name, 0)
			if err != nil {
				d := ioDiagnostic("font_unreadable", err, name)
				d.Items = fontItems[name]
				r.Diagnostics = append(r.Diagnostics, d)
				continue
			}
			font, err = typeset.LoadFont(data)
		}
		if err != nil {
			code := diag.Code("font_invalid")
			var embedding *typeset.EmbeddingError
			if errors.As(err, &embedding) {
				code = "font_not_embeddable"
			}
			d := diag.New(code, err.Error(), fontItems[name]...)
			if name != "default" {
				d.File = name
			}
			r.Diagnostics = append(r.Diagnostics, d)
		} else {
			fonts[name] = font
		}
	}
	var shaper typeset.Shaper
	for _, key := range textOrder {
		if c.Err() != nil {
			noteCancellation(c, r)
			return fonts
		}
		if font := fonts[key.font]; font != nil {
			if err := shaper.ValidateText(font, key.value); err != nil {
				d := textDiagnostic(err, "")
				d.Items = textItems[key]
				if key.font != "default" {
					d.File = key.font
				}
				r.Diagnostics = append(r.Diagnostics, d)
			}
		}
		if err := c.Err(); err != nil {
			r.Diagnostics = append(r.Diagnostics, diag.New("interrupted", err.Error()))
			break
		}
	}
	return fonts
}

func textDiagnostic(err error, item string) diag.Diagnostic {
	code := diag.Code("font_invalid")
	var text *typeset.TextError
	var overflow *typeset.OverflowError
	switch {
	case errors.As(err, &text):
		code = "text_unsupported_character"
		if text.GlyphMissingOnly() {
			code = "text_glyph_missing"
		}
	case errors.Is(err, typeset.ErrShapingFailed):
		code = "assembly_failed"
	case errors.As(err, &overflow):
		code = "text_overflow"
	}
	return diag.New(code, err.Error(), item)
}

type pageKey struct {
	size       job.Size
	text       job.TextStyle
	background job.Color
	filled     bool
}

func layOut(c context.Context, layout job.Layout, fonts map[string]*typeset.Font, r *result.Result) map[int]pdf.Page {
	pages := map[int]pdf.Page{}
	type cached struct {
		page pdf.Page
		err  error
	}
	cache := map[pageKey]cached{}
	var shaper typeset.Shaper
	for i, p := range layout.Parts {
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
		pages[i] = value.page
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
func placedTexts(pages map[int]pdf.Page) []*typeset.Placed {
	var placed []*typeset.Placed
	for _, p := range pages {
		if p.Text != nil {
			placed = append(placed, p.Text)
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

func resolvePolicies(layout job.Layout, facts map[string]pdf.SourceFacts, fit *job.Size, predict bool) []diag.Diagnostic {
	var ds []diag.Diagnostic
	formDefaults := ""
	names := map[string]bool{}
	for i, p := range layout.Parts {
		if p.Page != nil {
			continue
		}
		f := facts[p.Source]
		add := func(code diag.Code, message string) {
			d := diag.New(code, message, p.Pointer)
			d.File = p.Source
			ds = append(ds, d)
		}
		if fit != nil && f.HasWidgets {
			add("fit_form_unsupported", "Fitting source form widgets is unsupported.")
		}
		if f.HasFields {
			if formDefaults != "" && formDefaults != f.FormDefaultsDigest {
				add("source_form_unsupported", "Nonempty forms require identical resolved DR/DA/Q representations; these differ; assemble separately or flatten with a suitable PDF tool.")
			} else {
				formDefaults = f.FormDefaultsDigest
			}
			if predict && i > 0 {
				add("form_fields_renamed", "Predicted: pdfcpu may namespace this later form, including distinct names; build records actual mappings.")
			}
		}
		collision := false
		for _, name := range f.DestNames {
			if names[name] {
				collision = true
			}
			names[name] = true
		}
		if predict && collision {
			add("destinations_renamed", fmt.Sprintf("Predicted: colliding destination names in %s will be renamed.", p.Source))
		}
	}
	return ds
}
