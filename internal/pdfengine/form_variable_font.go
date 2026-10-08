// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Ervins Strauhmanis

package pdfengine

import (
	"context"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/benoitkugler/pdf/fonts/glyphsnames"
	"github.com/benoitkugler/pdf/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

type variableFont struct {
	codes  map[rune]byte
	widths [256]float64
	known  [256]bool
}

func compileVariableFont(ctx context.Context, pdf *model.Context, object types.Object) (*variableFont, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("variable font: %w", err)
	}

	dict, err := pdf.DereferenceDict(object)
	if err != nil {
		return nil, fmt.Errorf("%w: variable font dictionary: %w", errFormState, err)
	}

	if len(dict) == 0 {
		return nil, fmt.Errorf("%w: variable font dictionary is empty", errFormState)
	}

	if mapping, found := dict.Find("ToUnicode"); found && mapping != nil {
		return nil, fmt.Errorf("%w: variable font ToUnicode remapping is not supported", errFormState)
	}

	base, err := variableFontName(pdf, dict)
	if err != nil {
		return nil, err
	}

	encoding, err := variableFontEncoding(pdf, dict, base)
	if err != nil {
		return nil, err
	}

	font := &variableFont{codes: variableGlyphCodes(&encoding)}
	if widthErr := font.readWidths(ctx, pdf, dict, base, &encoding); widthErr != nil {
		return nil, widthErr
	}

	return font, nil
}

func variableFontName(pdf *model.Context, dict types.Dict) (string, error) {
	kind, _, err := pdf.DereferenceNameEntry(dict, keySubtype)
	if err != nil || kind == nil || *kind != nameType1 && *kind != "TrueType" {
		return "", fmt.Errorf("%w: variable appearance requires a supported simple font", errFormState)
	}

	name, _, err := pdf.DereferenceNameEntry(dict, keyBaseFont)
	if err != nil || name == nil {
		return "", fmt.Errorf("%w: variable font requires a BaseFont name", errFormState)
	}

	base := string(*name)
	if len(base) > 7 && base[6] == '+' {
		base = base[7:]
	}

	return base, nil
}

func variableGlyphCodes(encoding *simpleencodings.Encoding) map[rune]byte {
	codes := map[rune]byte{}

	for code, name := range encoding {
		if name == "" || name == ".notdef" {
			continue
		}

		value, found := glyphsnames.GlyphToRune(name)
		if !found || !utf8.ValidRune(value) {
			continue
		}

		if _, assigned := codes[value]; !assigned {
			codes[value] = byte(code)
		}
	}
	// PDF WinAnsi has duplicate space/hyphen glyph names for their nonbreaking Unicode forms.
	if encoding[160] == "space" {
		codes['\u00a0'] = 160
	}

	if encoding[173] == "hyphen" {
		codes['\u00ad'] = 173
	}

	return codes
}

func (f *variableFont) encode(ctx context.Context, text string) (variableAppearanceLine, error) {
	line := variableAppearanceLine{data: make([]byte, 0, len(text))}
	if !utf8.ValidString(text) {
		return line, fmt.Errorf("%w: variable field value must be valid Unicode", errFormState)
	}

	for _, value := range text {
		if err := ctx.Err(); err != nil {
			return line, fmt.Errorf("encode variable field: %w", err)
		}

		code, found := f.codes[value]
		if !found {
			return line, fmt.Errorf("%w: variable font has no glyph for U+%04X", errFormState, value)
		}

		if !f.known[code] {
			return line, fmt.Errorf("%w: variable font has no metric for U+%04X", errFormState, value)
		}

		line.data = append(line.data, code)
		line.width += f.widths[code]
	}

	if math.IsInf(line.width, 0) || math.IsNaN(line.width) {
		return line, fmt.Errorf("%w: variable text advance is not finite", errFormState)
	}

	return line, nil
}

func variableCoreEncoding(name string) simpleencodings.Encoding {
	switch name {
	case nameSymbol:
		return simpleencodings.Symbol
	case nameZapfDingbats:
		return simpleencodings.ZapfDingbats
	default:
		return simpleencodings.WinAnsi
	}
}
