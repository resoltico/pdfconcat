// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package core

import (
	"context"
	"errors"

	"github.com/resoltico/pdfconcat/internal/core/internal/diag"
	"github.com/resoltico/pdfconcat/internal/core/internal/files"
	"github.com/resoltico/pdfconcat/internal/core/internal/packet"
	"github.com/resoltico/pdfconcat/internal/core/internal/pdf"
	"github.com/resoltico/pdfconcat/internal/core/internal/result"
	"github.com/resoltico/pdfconcat/internal/core/internal/typeset"
)

func loadFonts(c context.Context, j packet.Job) (map[string]*typeset.Font, []diag.Diagnostic) {
	r := result.New("")
	fonts := map[string]*typeset.Font{}
	var fontOrder []string
	fontItems := map[string][]string{}
	fontPointers := map[string]map[string]bool{}
	type textKey struct{ font, value string }
	var textOrder []textKey
	textItems := map[textKey][]string{}
	textPointers := map[textKey]map[string]bool{}
	register := func(name, value string, pointers ...string) {
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
	leaves, sections := packet.Declarations(j)
	for _, p := range leaves {
		if p.Page != nil {
			register(p.Page.Text.Font, p.Page.Text.Value, p.Page.Origins["value"], p.Page.Origins["font"], p.Pointer)
		}
		if p.Contents != nil {
			register("default", p.Contents.Title, p.Pointer)
			for _, section := range sections {
				register("default", section.Title, section.Item+"/title", p.Pointer)
			}
		}
	}
	if j.PageNumbers != nil {
		register("default", "0123456789", "/page_numbers")
	}
	for _, name := range fontOrder {
		if c.Err() != nil {
			noteCancellation(c, &r)
			return fonts, r.Diagnostics
		}
		var font *typeset.Font
		var err error
		if name == "default" {
			font, err = typeset.LoadDefaultFont()
		} else {
			var data []byte
			data, err = files.Read(c, name, 0)
			if err != nil {
				d := ioDiagnostic(diag.FontUnreadable, err, name)
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
			noteCancellation(c, &r)
			return fonts, r.Diagnostics
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
			r.Diagnostics = append(r.Diagnostics, diag.New(diag.Interrupted, err.Error()))
			break
		}
	}
	return fonts, r.Diagnostics
}
