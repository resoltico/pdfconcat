// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 Ervins Strauhmanis

package app

import (
	"context"

	"github.com/resoltico/pdfconcat/internal/compose"

	packet "github.com/resoltico/pdfconcat/internal/core"
	"github.com/resoltico/pdfconcat/internal/core/diag"
	"github.com/resoltico/pdfconcat/internal/files"
	"github.com/resoltico/pdfconcat/internal/pdf"
)

func loadFonts(c context.Context, j packet.Plan) (compose.Fonts, []diag.Diagnostic) {
	var ds []diag.Diagnostic
	fonts := compose.Fonts{}
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
	for _, use := range j.TextUses() {
		register(use.Font, use.Text, use.Refs...)
	}
	for _, name := range fontOrder {
		if c.Err() != nil {
			ds = append(ds, diag.Report(diag.Interrupted, diag.InterruptedFailure(c.Err().Error())))
			return fonts, ds
		}
		var data []byte
		var err error
		if name != (packet.Font{}) {
			data, err = files.Read(c, name.Path, 0)
			if err != nil {
				d := ioDiagnostic(diag.FontUnreadable, err, name.Path)
				d.Items = fontItems[name]
				ds = append(ds, d)
				continue
			}
		}
		font, err := compose.ParseFont(name, data)
		if err == nil {
			err = pdf.ValidateFontName(font)
		}
		if err != nil {
			d := compose.Diagnostic(err, "")
			d.Items = fontItems[name]
			if name != (packet.Font{}) {
				d.File = name.Path
			}
			ds = append(ds, d)
		} else {
			fonts[name] = font
		}
	}
	preflight := compose.NewTextPreflight(j)
	for _, key := range textOrder {
		if c.Err() != nil {
			ds = append(ds, diag.Report(diag.Interrupted, diag.InterruptedFailure(c.Err().Error())))
			return fonts, ds
		}
		if font := fonts[key.font]; font != nil {
			if err := preflight.Check(font, key.value); err != nil {
				d := compose.Diagnostic(err, "")
				d.Items = textItems[key]
				if key.font != (packet.Font{}) {
					d.File = key.font.Path
				}
				ds = append(ds, d)
			}
		}
		if err := c.Err(); err != nil {
			ds = append(ds, diag.Report(diag.Interrupted, diag.InterruptedFailure(err.Error())))
			break
		}
	}
	return fonts, ds
}
