// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"errors"

	"github.com/resoltico/pdfconcat/internal/core/internal/compose"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func loadFonts(c context.Context, j packet.Plan) (map[packet.Font]*typeset.Font, []diag.Diagnostic) {
	r := result.New("")
	fonts := map[packet.Font]*typeset.Font{}
	var fontOrder []packet.Font
	fontItems := map[packet.Font][]string{}
	fontPointers := map[packet.Font]map[string]bool{}
	type textKey struct {
		font  packet.Font
		value string
	}
	var textOrder []textKey
	textItems := map[textKey][]string{}
	textPointers := map[textKey]map[string]bool{}
	register := func(name packet.Font, value string, pointers ...string) {
		if _, ok := fontPointers[name]; !ok {
			fontOrder = append(fontOrder, name)
			fontPointers[name] = map[string]bool{}
		}
		for _, pointer := range pointers {
			if pointer != "" && !fontPointers[name][pointer] {
				fontItems[name] = append(fontItems[name], pointer)
				fontPointers[name][pointer] = true
			}
		}
		if value == "" {
			return
		}
		key := textKey{font: name, value: value}
		if _, ok := textPointers[key]; !ok {
			textOrder = append(textOrder, key)
			textPointers[key] = map[string]bool{}
		}
		for _, pointer := range pointers {
			if pointer != "" && !textPointers[key][pointer] {
				textItems[key] = append(textItems[key], pointer)
				textPointers[key][pointer] = true
			}
		}
	}
	leaves, sections := j.Leaves(), j.Sections()
	for _, p := range leaves {
		if p.Page != nil {
			register(p.Page.Text.Font.Value, p.Page.Text.Value.Value, p.Page.Text.Value.From, p.Page.Text.Font.From, p.Ref)
		}
		if p.Contents != nil {
			register(packet.Font{}, p.Contents.Title, p.Ref)
			for _, section := range sections {
				register(packet.Font{}, section.Title, section.Item+"/title", p.Ref)
			}
		}
	}
	if j.PageNumbers != nil {
		register(packet.Font{}, "0123456789", "/page_numbers")
	}
	for _, name := range fontOrder {
		if c.Err() != nil {
			noteCancellation(c, &r)
			return fonts, r.Diagnostics
		}
		var font *typeset.Font
		var err error
		if name == (packet.Font{}) {
			font, err = typeset.LoadDefaultFont()
		} else {
			var data []byte
			data, err = files.Read(c, name.Path, 0)
			if err != nil {
				d := ioDiagnostic(diag.FontUnreadable, err, name.Path)
				d.Items = fontItems[name]
				r.Diagnostics = append(r.Diagnostics, d)
				continue
			}
			font, err = typeset.LoadFont(data)
		}
		if err == nil {
			err = pdf.ValidateFontName(font)
		}
		if err != nil {
			code := diag.FontInvalid
			var embedding *typeset.EmbeddingError
			if errors.As(err, &embedding) {
				code = diag.FontNotEmbeddable
			}
			d := diag.New(code, err.Error(), fontItems[name]...)
			if name != (packet.Font{}) {
				d.File = name.Path
			}
			r.Diagnostics = append(r.Diagnostics, d)
		} else {
			fonts[name] = font
		}
	}
	var shaper typeset.Shaper
	for _, key := range textOrder {
		if c.Err() != nil {
			noteCancellation(c, &r)
			return fonts, r.Diagnostics
		}
		if font := fonts[key.font]; font != nil {
			if err := shaper.ValidateText(font, key.value); err != nil {
				d := compose.Diagnostic(err, "")
				d.Items = textItems[key]
				if key.font != (packet.Font{}) {
					d.File = key.font.Path
				}
				r.Diagnostics = append(r.Diagnostics, d)
			}
		}
		if err := c.Err(); err != nil {
			r.Diagnostics = append(r.Diagnostics, diag.New(diag.Interrupted, err.Error()))
			break
		}
	}
	return fonts, r.Diagnostics
}
